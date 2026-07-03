package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/metrics"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/observability"
	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

type Handler interface {
	Handle(ctx context.Context, cmd apiv1.Command) apiv1.Response
}

type Client struct {
	gatewayURL      string
	identity        auth.Identity
	clusterName     string
	version         string
	logger          *zap.Logger
	handler         Handler
	commandTimeout  time.Duration
	heartbeatPeriod time.Duration
	dialer          Dialer
	writeMu         sync.Mutex
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
}

func NewClient(opts Options) *Client {
	return &Client{
		gatewayURL:      opts.GatewayURL,
		identity:        opts.Identity,
		clusterName:     opts.ClusterName,
		version:         opts.Version,
		logger:          opts.Logger,
		handler:         opts.Handler,
		commandTimeout:  opts.CommandTimeout,
		heartbeatPeriod: opts.HeartbeatPeriod,
		dialer:          websocket.DefaultDialer,
	}
}

func (c *Client) Run(ctx context.Context) error {
	backoff := NewBackoff(time.Second, 30*time.Second)
	connectedOnce := false
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
		if connectedOnce {
			metrics.IncGatewayReconnect()
		}
		connectedOnce = true
		c.logger.Info("connected to IncidentFlow Agent Gateway")
		metrics.SetGatewayConnected(true)
		backoff.Reset()
		err = c.serveConnection(ctx, conn)
		metrics.SetGatewayConnected(false)
		_ = conn.Close()
		if errors.Is(err, context.Canceled) {
			return err
		}
		c.logger.Warn("gateway connection closed; reconnecting", zap.Error(err))
	}
}

func (c *Client) serveConnection(ctx context.Context, conn *websocket.Conn) error {
	// connCtx is cancelled when this connection ends, stopping in-flight handlers.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	var wg sync.WaitGroup
	defer wg.Wait() // drain in-flight handleCommand goroutines before conn.Close()

	errCh := make(chan error, 2)
	go c.heartbeat(connCtx, conn, errCh)
	go c.readLoop(connCtx, conn, &wg, errCh)

	select {
	case <-ctx.Done():
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"),
			time.Now().Add(5*time.Second),
		)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
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
				errCh <- pingErr
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, wg *sync.WaitGroup, errCh chan<- error) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			errCh <- err
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.handleCommand(ctx, conn, cmd)
		}()
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
