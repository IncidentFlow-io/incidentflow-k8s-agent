package gateway

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/auth"
)

type Dialer interface {
	DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error)
}

func dialWebSocket(ctx context.Context, dialer Dialer, gatewayURL string, identity auth.Identity, clusterName, version string, heartbeatPeriod time.Duration) (*websocket.Conn, error) {
	parsed, err := url.Parse(gatewayURL)
	if err != nil {
		return nil, err
	}
	q := parsed.Query()
	if identity.AgentID != "" {
		q.Set("agent_id", identity.AgentID)
	}
	if identity.ClusterID != "" {
		q.Set("cluster_id", identity.ClusterID)
	}
	q.Set("cluster_name", clusterName)
	q.Set("version", version)
	parsed.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+identity.Token)
	headers.Set("User-Agent", "incidentflow-k8s-agent/"+version)
	conn, _, err := dialer.DialContext(ctx, parsed.String(), headers)
	if err != nil {
		return nil, err
	}
	// pongWait must be larger than heartbeatPeriod so the connection isn't
	// closed between pings. Use 3× the period with a 2-minute floor.
	pongWait := 3 * heartbeatPeriod
	if pongWait < 2*time.Minute {
		pongWait = 2 * time.Minute
	}
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MB per message
	configureHeartbeat(conn, pongWait)
	return conn, nil
}
