// Package zlog is an slog.Handler implementation focused on performant
// contextual logging.
//
// # Journald
//
// On Linux sytems, this package will automatically upgrade to speaking the
// [native Journald protocol] using the heuristic outlined on systemd.io. For
// this process, some information must be gathered via proc(5); exotic runtime
// configurations may not support this. The values "wmem_default" and "wmem_max"
// are consulted to determine optimal settings for the opened socket to journald
// and for when the memfd-based (see memfd_create(2) and unix(7)) protocol must
// be used.
//
// # Prose output
//
// If ProseFormat is set, output will be in prose rather than JSON.
// The field order is not configurable.
// ANSI color codes and [terminal hyperlinks] will be used when attached to a TTY.
// The environment variables "[NO_COLOR]" and "ZLOG_COLORS" can be used to
// control colors.
// Log records are separated by a ␞, fields are separated by a ␟, and the
// attributes are separated from the message with a ␝. These [field separators]
// may trip up incorrect programs.
//
// # ZLOG_COLORS
//
// The "ZLOG_COLORS" environment variable is akin to "[LS_COLORS]".
// It is a colon-delimited series of [SGR] parameters.
// Any characters outside of the range [0-;] will be ignored.
// The controllable colors are, in order:
//
//   - Error Level
//   - Warn Level
//   - Info Level
//   - Debug Level
//   - Source
//   - Timestamp
//   - Message
//   - Key
//   - string / [fmt.Stringer]
//   - bool (true)
//   - bool (false)
//   - Number (int64/uint64/float64)
//   - [time.Time]
//   - [time.Duration]
//   - error
//   - [encoding.TextUnmarshaler]
//   - [fmt.GoStringer]
//   - [encoding.BinaryUnmarshaler] / []byte
//   - [json.Unmarshaler]
//   - [fmt.Print]
//
// All left-ward elements must be present, but may be empty. For example, to highlight
// only errors:
//
//	ZLOG_COLORS='::::::::::::::5';
//	export ZLOG_COLORS
//
// See [DefaultProseColors] for the default colors.
//
// [native Journald protocol]: https://systemd.io/JOURNAL_NATIVE_PROTOCOL/
// [terminal hyperlinks]: https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda
// [NO_COLOR]: https://no-color.org/
// [LS_COLORS]: https://www.gnu.org/software/coreutils/manual/coreutils.html#dircolors-invocation
// [field separators]: https://en.wikipedia.org/wiki/C0_and_C1_control_codes#Field_separators
// [SGR]: https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_(Select_Graphic_Rendition)_parameters
package zlog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"runtime/pprof"

	"go.opentelemetry.io/otel/baggage"
)

// CtxKey is the type for Context keys.
type ctxKey struct{}

// CtxLevel is for per-Context log levels.
var ctxLevel ctxKey

// WithLevel overrides the minimum log level for all records created with the
// returned context.
func WithLevel(ctx context.Context, l slog.Level) context.Context {
	return context.WithValue(ctx, &ctxLevel, l)
}

// Some extra [slog.Level] aliases and syslog(3) compatible levels (as
// implemented in this package).
//
// The syslog mapping attempts to keep the slog convention of a 4-count gap
// between levels.
var (
	// Everything is just a nice low number to almost certainly catch anything
	// emitted.
	LevelEverything = slog.Level(-100)

	SyslogDebug    = slog.LevelDebug
	SyslogInfo     = slog.LevelInfo
	SyslogNotice   = slog.LevelInfo + 2
	SyslogWarning  = slog.LevelWarn
	SyslogError    = slog.LevelError
	SyslogCritical = slog.LevelError + 4
	SyslogAlert    = slog.LevelError + 8
	// Emergency is documented as "a panic condition".
	//
	// This package does no special handling for Go panics.
	SyslogEmergency = slog.LevelError + 12
)

// Handler is the concrete type for the [slog.Handler] objects returned by this
// package.
type handler[S state] struct {
	noCopy noCopy

	out  io.Writer
	opts *Options
	// Fmt is a pointer to function pointers to avoid making this struct
	// gigantic.
	fmt *formatter[S]
	// Pool is a pointer to the global pool for the state type.
	pool *statePool[S]

	prefmt *buffer
	groups []string
}

// NewHandler returns an [slog.Handler] emitting records to "w", according to the
// provided options.
//
// If "nil" is passed for options, suitable defaults will be used.
// On Linux systems, the journald native protocol will be used if the process is
// launched with the appropriate environment variables and the passed
// [io.Writer] is [os.Stderr].
// The default for a process running inside a Kubernetes container or as systemd
// service is to not emit timestamps.
func NewHandler(w io.Writer, opts *Options) slog.Handler {
	if opts == nil {
		opts = &Options{}
		opts.OmitTime = inK8s() || journalStream()
	}
	if h, ok := tryJournal(w, opts); ok {
		return h
	}

	if opts.ProseFormat {
		return proseHandler(w, opts)
	}

	return &handler[*stateJSON]{
		out:  &syncWriter{Writer: w},
		opts: opts,
		fmt:  &formatterJSON,
		pool: getPool[*stateJSON](),
	}
}

// Options is used to configure the [slog.Handler] returned by [NewHandler].
type Options struct {
	// Level is the minimum level that a log message must have to be processed
	// by the Handler.
	//
	// This can be overridden on a per-message basis by [WithLevel].
	Level slog.Leveler
	// Baggage is a selection function for keys in the OpenTelemetry Baggage
	// contained in the [context.Context] used with a log message.
	Baggage func(key string) bool
	// WriteError is a hook for receiving errors that occurred while attempting
	// to write the log message.
	//
	// The [slog] logging methods current do not have any means of reporting the
	// errors that Handler implementations return.
	WriteError func(context.Context, error)
	// OmitSource controls whether source position information should be
	// emitted.
	OmitSource bool
	// OmitTime controls whether a timestamp should be emitted.
	OmitTime bool
	//  ProseFormat controls whether the lines should be emitted in prose or
	//  JSON format.
	//
	// When connected to the Journal, this setting has no effect.
	ProseFormat bool

	// ForceANSI is a hook for testing to force ANSI color output.
	forceANSI bool
}

// Enabled implements [slog.Handler].
func (h *handler[S]) Enabled(ctx context.Context, l slog.Level) bool {
	min := slog.LevelInfo
	if h.opts.Level != nil {
		min = h.opts.Level.Level()
	}
	if cl, ok := ctx.Value(&ctxLevel).(slog.Level); ok {
		min = cl
	}
	return l >= min
}

// Handle implements [slog.Handler].
func (h *handler[S]) Handle(ctx context.Context, r slog.Record) (err error) {
	b := newBuffer()
	defer b.Release()
	s := h.pool.Get(h.groups, h.prefmt)
	defer h.pool.Put(s)
	h.fmt.Start(b, s)

	// Default keys:
	// Level
	h.fmt.WriteLevel(b, s, r.Level)
	// "source"
	if !h.opts.OmitSource && r.PC != 0 {
		frames := runtime.CallersFrames([]uintptr{r.PC})
		frame, _ := frames.Next()
		h.fmt.WriteSource(b, s, &frame)
	}
	// Time, if emitting
	if !h.opts.OmitTime && !r.Time.IsZero() {
		h.fmt.WriteTime(b, s, r.Time)
	}
	// "msg"
	h.fmt.WriteMessage(b, s, r.Message)

	// Add baggage if filter function is present.
	if f := h.opts.Baggage; f != nil {
		g := false
		bg := baggage.FromContext(ctx)
		for _, m := range bg.Members() {
			if !f(m.Key()) {
				continue
			}
			if !g {
				h.fmt.PushGroup(b, s, h.fmt.BaggageKey)
				g = true
			}
			h.fmt.AppendKey(b, s, m.Key())
			h.fmt.AppendString(b, s, m.Value())
		}
		if g {
			h.fmt.PopGroup(b, s)
		}
	}
	// Add pprof labels if present.
	ls := make([][2]string, 0, 10) // Guess at capacity.
	pprof.ForLabels(ctx, func(k, v string) bool {
		ls = append(ls, [2]string{k, v})
		return true
	})
	if len(ls) != 0 {
		h.fmt.PushGroup(b, s, h.fmt.PprofKey)
		for _, l := range ls {
			h.fmt.AppendKey(b, s, l[0])
			h.fmt.AppendString(b, s, l[1])
		}
		h.fmt.PopGroup(b, s)
	}

	// Add the attached Attrs.
	if h.prefmt != nil {
		b.Write(*h.prefmt)
	}
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(b, s, a)
		return true
	})

	// Finish and send.
	h.fmt.End(b, s)
	var n int
	n, err = h.out.Write(*b)
	if n != len(*b) && errors.Is(err, nil) {
		err = io.ErrShortWrite
	}
	if err != nil && h.opts.WriteError != nil {
		h.opts.WriteError(ctx, err)
	}
	return err
}

// AppendAttr fully resolves the Attr value, then calls the appropriate
// formatting hooks.
func (h *handler[S]) appendAttr(b *buffer, s S, a slog.Attr) error {
	a.Value = a.Value.Resolve()
	kind := a.Value.Kind()
	if kind != slog.KindGroup {
		if a.Key == "" {
			return nil
		}
		h.fmt.AppendKey(b, s, a.Key)
	}
	switch v := a.Value; kind {
	case slog.KindBool:
		h.fmt.AppendBool(b, s, v.Bool())
	case slog.KindInt64:
		h.fmt.AppendInt64(b, s, v.Int64())
	case slog.KindUint64:
		h.fmt.AppendUint64(b, s, v.Uint64())
	case slog.KindFloat64:
		h.fmt.AppendFloat64(b, s, v.Float64())
	case slog.KindString:
		h.fmt.AppendString(b, s, v.String())
	case slog.KindDuration:
		h.fmt.AppendDuration(b, s, v.Duration())
	case slog.KindTime:
		h.fmt.AppendTime(b, s, v.Time())
	case slog.KindGroup:
		attrs := v.Group()
		if len(attrs) != 0 {
			if a.Key != "" {
				h.fmt.PushGroup(b, s, a.Key)
			}
			for _, ga := range attrs {
				h.appendAttr(b, s, ga)
			}
			if a.Key != "" {
				h.fmt.PopGroup(b, s)
			}
		}
	case slog.KindAny:
		return h.fmt.AppendAny(b, s, v.Any())
	default:
		panic("unimplemented Kind: " + kind.String())
	}
	return nil
}

// WithAttrs implements [slog.Handler].
func (h *handler[S]) WithAttrs(attrs []slog.Attr) slog.Handler {
	p := h.prefmt.Clone()
	s := h.pool.Get(h.groups, h.prefmt)
	defer h.pool.Put(s)
	for _, a := range attrs {
		h.appendAttr(p, s, a)
	}
	return &handler[S]{
		out:    h.out,
		opts:   h.opts,
		fmt:    h.fmt,
		pool:   h.pool,
		prefmt: p,
		groups: h.groups,
	}
}

// WithGroup implements [slog.Handler].
func (h *handler[S]) WithGroup(name string) slog.Handler {
	p := h.prefmt.Clone()
	s := h.pool.Get(h.groups, nil)
	defer h.pool.Put(s)
	h.fmt.PushGroup(p, s, name)
	return &handler[S]{
		out:    h.out,
		opts:   h.opts,
		fmt:    h.fmt,
		pool:   h.pool,
		prefmt: p,
		groups: append(h.groups, name),
	}
}
