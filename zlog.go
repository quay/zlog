// Package zlog is a logging facade backed by zerolog.
//
// It uses opentelemetry baggage to generate log contexts.
//
// By default, the package wraps the zerolog global logger. This can be changed
// via the Set function.
//
// In addition, a testing adapter is provided to keep testing logs orderly.
package zlog

import (
	"context"

	"github.com/rs/zerolog"
	global "github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/label"
)

// Log is the logger used by the package-level functions.
var log = &global.Logger

// Set configures the logger used by this package.
//
// This function is unsafe to use concurrently with the other functions of this
// package.
func Set(l *zerolog.Logger) {
	log = l
}

// AddCtx is the workhorse function that every facade function calls.
//
// If the passed Event is enabled, it will attach all the otel baggage to
// it and return it.
func addCtx(ctx context.Context, ev *zerolog.Event) *zerolog.Event {
	if !ev.Enabled() {
		return ev
	}

	s := baggage.Set(ctx)
	for i := s.Iter(); i.Next(); {
		kv := i.Label()
		k := string(kv.Key)
		v := kv.Value
		switch v.Type() {
		case label.BOOL:
			ev.Bool(k, v.AsBool())
		case label.INT32:
			ev.Int32(k, v.AsInt32())
		case label.INT64:
			ev.Int64(k, v.AsInt64())
		case label.UINT32:
			ev.Uint32(k, v.AsUint32())
		case label.UINT64:
			ev.Uint64(k, v.AsUint64())
		case label.FLOAT32:
			ev.Float32(k, v.AsFloat32())
		case label.FLOAT64:
			ev.Float64(k, v.AsFloat64())
		case label.STRING:
			ev.Str(k, v.AsString())
		case label.ARRAY:
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
	}

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
