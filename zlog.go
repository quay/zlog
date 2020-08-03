// Package zlog is a logging facade backed by zerolog.
//
// It uses opentelemetry correlations to generate log contexts.
//
// The package wraps the zerolog global logger, so an application's main should
// do configuration there.
//
// In addition, a testing adapter is provided to keep testing logs orderly.
package zlog

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/api/correlation"
	"go.opentelemetry.io/otel/api/kv"
	"go.opentelemetry.io/otel/api/kv/value"
)

// AddCtx is the workhorse function that every facade function calls.
//
// If the passed Event is enabled, it will attach all the otel correlations to
// it and return it.
func addCtx(ctx context.Context, ev *zerolog.Event) *zerolog.Event {
	if !ev.Enabled() {
		return ev
	}

	correlation.MapFromContext(ctx).Foreach(func(kv kv.KeyValue) bool {
		k := string(kv.Key)
		v := kv.Value
		switch v.Type() {
		case value.BOOL:
			ev.Bool(k, v.AsBool())
		case value.INT32:
			ev.Int32(k, v.AsInt32())
		case value.INT64:
			ev.Int64(k, v.AsInt64())
		case value.UINT32:
			ev.Uint32(k, v.AsUint32())
		case value.UINT64:
			ev.Uint64(k, v.AsUint64())
		case value.FLOAT32:
			ev.Float32(k, v.AsFloat32())
		case value.FLOAT64:
			ev.Float64(k, v.AsFloat64())
		case value.STRING:
			ev.Str(k, v.AsString())
		case value.ARRAY:
			za := zerolog.Arr()
			switch a := v.AsArray().(type) {
			case []bool:
				for _, v := range a {
					za.Bool(v)
				}
			case []string:
				for _, v := range a {
					za.Str(v)
				}
			case []int:
				for _, v := range a {
					za.Int(v)
				}
			case []int32:
				for _, v := range a {
					za.Int32(v)
				}
			case []int64:
				for _, v := range a {
					za.Int64(v)
				}
			case []uint:
				for _, v := range a {
					za.Uint(v)
				}
			case []uint32:
				for _, v := range a {
					za.Uint32(v)
				}
			case []uint64:
				for _, v := range a {
					za.Uint64(v)
				}
			case []float32:
				for _, v := range a {
					za.Float32(v)
				}
			case []float64:
				for _, v := range a {
					za.Float64(v)
				}
			}
			ev.Array(k, za)
		}
		return true
	})
	return ev
}

// Log starts a new message with no level.
func Log(ctx context.Context) *zerolog.Event {
	return addCtx(ctx, log.Log())
}

// WithLevel starts a new message with the specified level.
func WithLevel(ctx context.Context, l zerolog.Level) *zerolog.Event {
	return addCtx(ctx, log.WithLevel(l))
}

// Trace starts a new message with the trace level.
func Trace(ctx context.Context) *zerolog.Event {
	return addCtx(ctx, log.Trace())
}

// Debug starts a new message with the debug level.
func Debug(ctx context.Context) *zerolog.Event {
	return addCtx(ctx, log.Debug())
}

// Info starts a new message with the infor level.
func Info(ctx context.Context) *zerolog.Event {
	return addCtx(ctx, log.Info())
}

// Warn starts a new message with the warn level.
func Warn(ctx context.Context) *zerolog.Event {
	return addCtx(ctx, log.Warn())
}

// Error starts a new message with the error level.
func Error(ctx context.Context) *zerolog.Event {
	return addCtx(ctx, log.Error())
}
