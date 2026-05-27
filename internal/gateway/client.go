package gateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
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
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := dialWebSocket(ctx, c.dialer, c.gatewayURL, c.identity, c.clusterName, c.version)
		if err != nil {
			delay := backoff.Next()
			c.logger.Warn("gateway connection failed", zap.Error(err), zap.Duration("retry_in", delay))
			if !sleepContext(ctx, delay) {
				return ctx.Err()
			}
			continue
		}
		c.logger.Info("connected to IncidentFlow Agent Gateway")
		backoff.Reset()
		err = c.serveConnection(ctx, conn)
		_ = conn.Close()
		if errors.Is(err, context.Canceled) {
			return err
		}
		c.logger.Warn("gateway connection closed; reconnecting", zap.Error(err))
	}
}

func (c *Client) serveConnection(ctx context.Context, conn *websocket.Conn) error {
	errCh := make(chan error, 2)
	go c.heartbeat(ctx, conn, errCh)
	go c.readLoop(ctx, conn, errCh)
	select {
	case <-ctx.Done():
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), time.Now().Add(5*time.Second))
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
			err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second))
			c.writeMu.Unlock()
			if err != nil {
				errCh <- err
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, errCh chan<- error) {
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
		go c.handleCommand(ctx, conn, cmd)
	}
}

func (c *Client) handleCommand(parent context.Context, conn *websocket.Conn, cmd apiv1.Command) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, c.commandTimeout)
	defer cancel()
	resp := c.handler.Handle(ctx, cmd)
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
