package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/observability"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/telemetry"
	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

const defaultMaxConcurrentCommands = 8

type Handler interface {
	Handle(ctx context.Context, cmd apiv1.Command) apiv1.Response
}

type Client struct {
	gatewayURL            string
	identity              auth.Identity
	clusterName           string
	version               string
	logger                *zap.Logger
	handler               Handler
	commandTimeout        time.Duration
	heartbeatPeriod       time.Duration
	dialer                Dialer
	writeMu               sync.Mutex
	maxConcurrentCommands int
}

type Options struct {
	GatewayURL      string
	Identity        auth.Identity
	ClusterName     string
	Version         string
	Logger          *zap.Logger
	Handler         Handler
	CommandTimeout  time.Duration
	HeartbeatPeriod time.Duration
	// MaxConcurrentCommands bounds in-flight Kubernetes commands per connection.
	// Zero uses a safe default.
	MaxConcurrentCommands int
}

func NewClient(opts Options) *Client {
	maxConcurrentCommands := opts.MaxConcurrentCommands
	if maxConcurrentCommands <= 0 {
		maxConcurrentCommands = defaultMaxConcurrentCommands
	}
	return &Client{
		gatewayURL:            opts.GatewayURL,
		identity:              opts.Identity,
		clusterName:           opts.ClusterName,
		version:               opts.Version,
		logger:                opts.Logger,
		handler:               opts.Handler,
		commandTimeout:        opts.CommandTimeout,
		heartbeatPeriod:       opts.HeartbeatPeriod,
		dialer:                websocket.DefaultDialer,
		maxConcurrentCommands: maxConcurrentCommands,
	}
}

func (c *Client) Run(ctx context.Context) error {
	backoff := NewBackoff(time.Second, 30*time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := dialWebSocket(ctx, c.dialer, c.gatewayURL, c.identity, c.clusterName, c.version, c.heartbeatPeriod)
		if err != nil {
			delay := backoff.Next()
			c.logger.Warn("gateway connection failed", zap.Error(err), zap.Duration("retry_in", delay))
			if !sleepContext(ctx, delay) {
				return ctx.Err()
			}
			continue
		}
		c.logger.Info("connected to IncidentFlow Agent Gateway")
		telemetry.GatewayConnected.Set(1)
		backoff.Reset()
		err = c.serveConnection(ctx, conn)
		telemetry.GatewayConnected.Set(0)
		if errors.Is(err, context.Canceled) {
			return err
		}
		telemetry.GatewayReconnects.Inc()
		c.logger.Warn("gateway connection closed; reconnecting", zap.Error(err))
	}
}

func (c *Client) serveConnection(ctx context.Context, conn *websocket.Conn) error {
	// Cancelling connCtx stops all commands from this connection. Closing the
	// socket unblocks ReadMessage and any in-progress write immediately.
	connCtx, connCancel := context.WithCancel(ctx)
	var commandWG sync.WaitGroup
	var loopWG sync.WaitGroup
	commandSem := make(chan struct{}, c.maxConcurrentCommands)

	errCh := make(chan error, 2)
	loopWG.Add(2)
	go func() {
		defer loopWG.Done()
		c.heartbeat(connCtx, conn, errCh)
	}()
	go func() {
		defer loopWG.Done()
		c.readLoop(connCtx, conn, &commandWG, commandSem, errCh)
	}()

	var result error
	select {
	case <-ctx.Done():
		result = ctx.Err()
	case err := <-errCh:
		result = err
	}
	connCancel()
	_ = conn.Close()
	loopWG.Wait()
	commandWG.Wait()
	return result
}

func (c *Client) heartbeat(ctx context.Context, conn *websocket.Conn, errCh chan<- error) {
	ticker := time.NewTicker(c.heartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.writeMu.Lock()
			pingErr := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second))
			if pingErr == nil {
				hbMsg, _ := json.Marshal(map[string]string{"type": "heartbeat"})
				pingErr = conn.WriteMessage(websocket.TextMessage, hbMsg)
			}
			c.writeMu.Unlock()
			if pingErr != nil {
				reportConnectionError(ctx, errCh, pingErr)
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, wg *sync.WaitGroup, commandSem chan struct{}, errCh chan<- error) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			reportConnectionError(ctx, errCh, err)
			return
		}
		cmd, err := DecodeCommand(data)
		if err != nil {
			c.logger.Warn("discarding invalid gateway message", zap.Error(err))
			continue
		}
		c.logger.Info("received command from gateway",
			zap.String("command_id", cmd.ID),
			zap.String("action", cmd.Action),
		)
		select {
		case commandSem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-commandSem }()
			c.handleCommand(ctx, conn, cmd)
		}()
	}
}

// reportConnectionError never blocks a shutdown path. The first error wakes
// serveConnection; subsequent errors are irrelevant once the socket is closed.
func reportConnectionError(ctx context.Context, errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-ctx.Done():
	default:
	}
}

func (c *Client) handleCommand(ctx context.Context, conn *websocket.Conn, cmd apiv1.Command) {
	started := time.Now()

	// Extract distributed trace context from the command payload (injected by agent-gateway).
	if cmd.Traceparent != "" {
		carrier := propagation.MapCarrier{
			"traceparent": cmd.Traceparent,
			"tracestate":  cmd.Tracestate,
		}
		prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})
		ctx = prop.Extract(ctx, carrier)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, c.commandTimeout)
	defer cancel()

	cmdCtx, span := observability.Tracer.Start(cmdCtx, "k8s_agent.handle_command")
	defer span.End()
	span.SetAttributes(
		attribute.String("command.id", cmd.ID),
		attribute.String("command.action", cmd.Action),
	)

	resp := c.handler.Handle(cmdCtx, cmd)
	telemetry.K8sAPIDuration.
		WithLabelValues(cmd.Action, metricStatus(resp.Status)).
		Observe(time.Since(started).Seconds())
	span.SetAttributes(attribute.String("command.status", resp.Status))
	if resp.Status != apiv1.StatusSuccess {
		if resp.Error != nil {
			span.SetStatus(codes.Error, resp.Error.Code)
		} else {
			span.SetStatus(codes.Error, "command failed")
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}
	data, err := EncodeResponse(resp)
	if err != nil {
		c.logger.Error("encode command response", zap.String("command_id", cmd.ID), zap.Error(err))
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.logger.Warn("write command response failed", zap.String("command_id", cmd.ID), zap.Error(err))
		return
	}
	c.logger.Info("sent command response to gateway",
		zap.String("command_id", cmd.ID),
		zap.String("action", cmd.Action),
		zap.String("status", resp.Status),
		zap.Duration("duration", time.Since(started)),
	)
}

// metricStatus maps a command response status to the bounded label set used by
// the k8s_api_duration_seconds metric ("success" or "error").
func metricStatus(status string) string {
	if status == apiv1.StatusSuccess {
		return "success"
	}
	return "error"
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
