package muse

import (
	"context"
	"log/slog"
	"time"
)

type latencyKey struct{}
type latencyTrace struct {
	id    string
	start time.Time
}

// WithLatency joins transport, generation, synthesis and playback on one clock.
func WithLatency(ctx context.Context) context.Context {
	if ctx.Value(latencyKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, latencyKey{}, latencyTrace{id: uuid4(), start: time.Now()})
}

// LogLatency records only timing and caller-supplied numeric or categorical fields.
func LogLatency(ctx context.Context, logger *slog.Logger, stage string, fields ...any) {
	if ctx == nil {
		return
	}
	trace, ok := ctx.Value(latencyKey{}).(latencyTrace)
	if !ok {
		return
	}
	attrs := []any{"trace", trace.id, "stage", stage, "elapsed_ms", time.Since(trace.start).Milliseconds()}
	logger.Info("muse latency", append(attrs, fields...)...)
}
