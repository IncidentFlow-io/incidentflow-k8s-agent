package telemetry

import "sync/atomic"

type Metrics struct {
	commandsHandled atomic.Int64
	commandsFailed  atomic.Int64
	reconnects      atomic.Int64
}

func (m *Metrics) IncCommandsHandled() { m.commandsHandled.Add(1) }
func (m *Metrics) IncCommandsFailed()  { m.commandsFailed.Add(1) }
func (m *Metrics) IncReconnects()      { m.reconnects.Add(1) }
