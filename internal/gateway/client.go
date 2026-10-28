package gateway

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	defaultMaxConcurrentCommands = 8
	stableConnectionDuration     = 30 * time.Second
	commandReplayTTL             = 10 * time.Minute
	maxRememberedCommands        = 4096
)

// ErrAccessRevoked tells the app to obtain fresh registration credentials.
// Retrying the same gateway token cannot recover a revoked agent.
var ErrAccessRevoked = errors.New("agent gateway access revoked")

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
	onConnectionState     func(bool)
	onHeartbeat           func()
	seenCommands          map[string]time.Time
	seenCommandsMu        sync.Mutex
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
	// OnConnectionState is called when an active gateway connection closes.
	OnConnectionState func(bool)
	// OnHeartbeat is called only after a ping and heartbeat message were both
	// written successfully on the active gateway session.
	OnHeartbeat func()
}

func NewClient(opts Options) *Client {
	maxConcurrentCommands := opts.MaxConcurrentCommands
	if maxConcurrentCommands <= 0 {
		maxConcurrentCommands = defaultMaxConcurrentCommands
	}
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		gatewayURL:      opts.GatewayURL,
		identity:        opts.Identity,
		clusterName:     opts.ClusterName,
		version:         opts.Version,
		logger:          logger.With(zap.String("component", "gateway")),
		handler:         opts.Handler,
		commandTimeout:  opts.CommandTimeout,
		heartbeatPeriod: opts.HeartbeatPeriod,
		dialer: &websocket.Dialer{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// InsecureSkipVerify is deliberately left false. Do not add a
			// configuration escape hatch: the agent sends bearer credentials.
		}},
		maxConcurrentCommands: maxConcurrentCommands,
		seenCommands:          make(map[string]time.Time),
		onConnectionState:     opts.OnConnectionState,
		onHeartbeat:           opts.OnHeartbeat,
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
			if errors.Is(err, ErrAccessRevoked) {
				return err
			}
			delay := backoff.Next()
			c.logger.Warn("gateway connection failed", zap.Error(err), zap.Int64("retry_in_ms", delay.Milliseconds()))
			if !sleepContext(ctx, delay) {
				return ctx.Err()
			}
			continue
		}
		c.logger.Info("connected to IncidentFlow Agent Gateway")
		telemetry.GatewayConnected.Set(1)
		connectedAt := time.Now()
		err = c.serveConnection(ctx, conn)
		telemetry.GatewayConnected.Set(0)
		if c.onConnectionState != nil {
			c.onConnectionState(false)
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) && closeErr.Code == websocket.ClosePolicyViolation {
			return fmt.Errorf("%w: %v", ErrAccessRevoked, err)
		}
		if time.Since(connectedAt) >= stableConnectionDuration {
			backoff.Reset()
		}
		telemetry.GatewayReconnects.Inc()
		delay := backoff.Next()
		c.logger.Warn("gateway connection closed; reconnecting", zap.Error(err), zap.Int64("retry_in_ms", delay.Milliseconds()))
		if !sleepContext(ctx, delay) {
			return ctx.Err()
		}
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
	// Tell the gateway this session is going away before closing the TCP socket.
	// WriteControl is safe alongside a concurrent data writer.
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "Agent stopping"),
		time.Now().Add(time.Second),
	)
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
			if c.onHeartbeat != nil {
				c.onHeartbeat()
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
		if err := policyAllowsCommand(cmd); err != nil {
			c.logger.Warn("command denied by access policy",
				zap.String("command_id", cmd.ID), zap.String("action", cmd.Action), zap.Error(err))
			c.writeResponse(conn, apiv1.Failure(cmd.ID, apiv1.ErrPolicyDenied, err.Error()))
			continue
		}
		if !c.claimCommand(cmd.ID) {
			c.writeResponse(conn, apiv1.Failure(cmd.ID, apiv1.ErrDuplicateCommand, "command id was already processed"))
			continue
		}
		c.logger.Debug("received command from gateway",
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

func policyAllowsCommand(cmd apiv1.Command) error {
	// Namespace discovery is an internal, admin-authorized bootstrap operation
	// used to create the first access policy. It is not exposed as an MCP tool;
	// every normal Kubernetes action still requires an active workspace policy.
	if cmd.Action == "agent.discover_namespaces" {
		return nil
	}

	policy := cmd.AccessPolicy
	if policy == nil || policy.Version < 1 {
		return errors.New("no active workspace access policy")
	}
	if policy.NamespaceMode != "selected" && policy.NamespaceMode != "all" {
		return errors.New("invalid workspace access policy")
	}
	if cmd.Action == "k8s.get_pod_logs" && !policy.PodLogsEnabled {
		return errors.New("pod logs are disabled by the workspace policy")
	}
	if policy.NamespaceMode == "all" || cmd.Action == "k8s.list_namespaces" {
		return nil
	}
	var params struct {
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(cmd.Params, &params); err != nil {
		return errors.New("invalid command namespace")
	}
	if params.Namespace == "" {
		return errors.New("namespace is required by the workspace policy")
	}
	for _, allowed := range policy.Namespaces {
		if params.Namespace == allowed {
			return nil
		}
	}
	return errors.New("namespace is not allowed by the workspace policy")
}

func (c *Client) claimCommand(id string) bool {
	now := time.Now()
	c.seenCommandsMu.Lock()
	defer c.seenCommandsMu.Unlock()
	for commandID, seenAt := range c.seenCommands {
		if now.Sub(seenAt) > commandReplayTTL {
			delete(c.seenCommands, commandID)
		}
	}
	if _, exists := c.seenCommands[id]; exists {
		return false
	}
	if len(c.seenCommands) >= maxRememberedCommands {
		// Refuse rather than evicting a live replay-protection entry.
		return false
	}
	c.seenCommands[id] = now
	return true
}

func (c *Client) writeResponse(conn *websocket.Conn, resp apiv1.Response) {
	data, err := EncodeResponse(resp)
	if err != nil {
		c.logger.Error("encode command response", zap.String("command_id", resp.ID), zap.Error(err))
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.logger.Warn("write command response failed", zap.String("command_id", resp.ID), zap.Error(err))
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
	c.logger.Info("command completed",
		zap.String("command_id", cmd.ID),
		zap.String("action", cmd.Action),
		zap.String("status", resp.Status),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
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
