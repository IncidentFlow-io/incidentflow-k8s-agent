package gateway

import (
	"time"

	"github.com/gorilla/websocket"
)

func configureHeartbeat(conn *websocket.Conn, pongWait time.Duration) {
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
}
