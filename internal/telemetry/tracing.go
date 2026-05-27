package telemetry

import "context"

func WithTraceContext(ctx context.Context) context.Context {
	return ctx
}
