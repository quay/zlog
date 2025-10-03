package zlog

import (
	"context"
	"log/slog"
	"os"
	"runtime/pprof"
	"slices"

	"go.opentelemetry.io/otel/baggage"
)

var ExampleOptions = Options{
	Level:      LevelEverything,
	OmitTime:   true,
	OmitSource: true,
	ContextKey: SetAttrs,
	LevelKey:   SetLevel,
}

type ctxkey int

// Context key variables for the examples.
const (
	_ ctxkey = iota
	SetAttrs
	SetLevel
)

func Example() {
	h := NewHandler(os.Stdout, &ExampleOptions)
	slog.New(h).With("a", "b").Info("test", "c", "d")

	// Output:
	// {"level":"INFO","msg":"test","a":"b","c":"d"}
}

// In this example, some values are extracted from the pprof labels and inserted
// into the record.
func Example_pprof() {
	ctx := pprof.WithLabels(context.Background(), pprof.Labels("test_kind", "example"))
	pprof.SetGoroutineLabels(ctx)
	h := NewHandler(os.Stdout, &ExampleOptions)
	slog.New(h).InfoContext(ctx, "test")

	// Output:
	// {"level":"INFO","msg":"test","goroutine":{"test_kind":"example"}}
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

// In this example, some values are extracted from the OpenTelemetry baggage and
// inserted into the record.
func Example_baggage() {
	opts := ExampleOptions
	opts.Baggage = func(_ string) bool { return true }
	b := must(baggage.New(
		must(baggage.NewMember("test_kind", "example")),
	))
	ctx := baggage.ContextWithBaggage(context.Background(), b)
	h := NewHandler(os.Stdout, &opts)
	slog.New(h).InfoContext(ctx, "test")

	// Output:
	// {"level":"INFO","msg":"test","baggage":{"test_kind":"example"}}
}

// In this example, the handler is configured with a very high minimum level, so
// without the per-record level filtering there would be no log messages.
func Example_with_Level() {
	// Per-record filter levels.
	filters := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	// Levels of records to emit.
	levels := []slog.Level{slog.LevelDebug - 4, slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	// With is a helper function to add the log level to the Context at the known key.
	//
	// Typically, a module would provide a helper to do this.
	with := func(ctx context.Context, l slog.Level) context.Context {
		return context.WithValue(ctx, SetLevel, l)
	}

	// Setup:
	ctx := context.Background()
	opts := ExampleOptions
	opts.Level = slog.Level(100)
	h := NewHandler(os.Stdout, &opts)
	log := slog.New(h)

	// Usage:
	a := slog.String("filter", "NONE")
	for _, l := range levels {
		log.LogAttrs(ctx, l, "", a)
	}
	for _, l := range filters {
		a = slog.String("filter", l.String())
		ctx := with(ctx, l)
		for _, l := range levels {
			log.LogAttrs(ctx, l, "", a)
		}
	}

	// Output:
	// {"level":"DEBUG","msg":"","filter":"DEBUG"}
	// {"level":"INFO","msg":"","filter":"DEBUG"}
	// {"level":"WARN","msg":"","filter":"DEBUG"}
	// {"level":"ERROR","msg":"","filter":"DEBUG"}
	// {"level":"INFO","msg":"","filter":"INFO"}
	// {"level":"WARN","msg":"","filter":"INFO"}
	// {"level":"ERROR","msg":"","filter":"INFO"}
	// {"level":"WARN","msg":"","filter":"WARN"}
	// {"level":"ERROR","msg":"","filter":"WARN"}
	// {"level":"ERROR","msg":"","filter":"ERROR"}
}

// In this example, there are values stored in the Context at a known key and
// then automatically retrieved and integrated into the record by the handler.
func Example_with_Attrs() {
	// With is a helper function to add values to the Context at the known key.
	//
	// Typically, a module would provide a helper to do this, and do it with
	// less garbage. Any ordering or replacement semantics need to happen here;
	// this example does not implement being able to remove keys from the
	// Context.
	with := func(ctx context.Context, args ...any) context.Context {
		var s []slog.Attr
		if v, ok := ctx.Value(SetAttrs).(slog.Value); ok {
			s = v.Group()
		}
		s = append(s, slog.Group("", args...).Value.Group()...)
		seen := make(map[string]struct{}, len(s))
		del := func(a slog.Attr) bool {
			_, ok := seen[a.Key]
			seen[a.Key] = struct{}{}
			return ok
		}
		slices.Reverse(s)
		s = slices.DeleteFunc(s, del)
		slices.Reverse(s)
		return context.WithValue(ctx, SetAttrs, slog.GroupValue(s...))
	}
	// Setup:
	ctx := context.Background()
	h := NewHandler(os.Stdout, &ExampleOptions)
	l := slog.New(h)

	// Usage:
	l.InfoContext(ctx, "without ctx attrs", "a", "b")
	{
		ctx := with(ctx, "contextual", "value")
		l.InfoContext(ctx, "with ctx attrs", "a", "b")
		{
			ctx := context.WithValue(ctx, SetLevel, slog.LevelDebug)
			ctx = with(ctx, "contextual", "level")
			l.DebugContext(ctx, "with ctx attrs", "a", "b")
		}
		ctx = with(ctx, "appended", "value")
		l.InfoContext(ctx, "with more ctx attrs")
	}
	l.InfoContext(ctx, "without ctx attrs", "a", "b")

	// Output:
	// {"level":"INFO","msg":"without ctx attrs","a":"b"}
	// {"level":"INFO","msg":"with ctx attrs","contextual":"value","a":"b"}
	// {"level":"DEBUG","msg":"with ctx attrs","contextual":"level","a":"b"}
	// {"level":"INFO","msg":"with more ctx attrs","contextual":"value","appended":"value"}
	// {"level":"INFO","msg":"without ctx attrs","a":"b"}
}
