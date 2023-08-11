package zlog

import (
	"log/slog"
	"runtime"
	"time"
)

// Formatter is a struct that contains all the hooks for emitting records in a
// given format.
//
// This style is done over an interface for no particular reason.
type formatter[S state] struct {
	// Names for the contextual groups.
	PprofKey   string
	BaggageKey string

	// Lifecycle hooks:
	Start func(*buffer, S)
	End   func(*buffer, S)

	// Writing hooks:
	AppendKey      func(*buffer, S, string)
	AppendString   func(*buffer, S, string)
	AppendBool     func(*buffer, S, bool)
	AppendInt64    func(*buffer, S, int64)
	AppendUint64   func(*buffer, S, uint64)
	AppendFloat64  func(*buffer, S, float64)
	AppendTime     func(*buffer, S, time.Time)
	AppendDuration func(*buffer, S, time.Duration)
	AppendAny      func(*buffer, S, any) error

	// Write* functions are special in that they're _only_ called with a value
	// generated by the Handler.
	WriteSource  func(*buffer, S, *runtime.Frame)
	WriteLevel   func(*buffer, S, slog.Level)
	WriteMessage func(*buffer, S, string)
	WriteTime    func(*buffer, S, time.Time)

	// Grouping hooks:
	PushGroup func(*buffer, S, string)
	PopGroup  func(*buffer, S)
}

// State is an object that's used per-record to keep track of formatting.
type state interface {
	Reset(groups []string, prefmt *buffer)
}
