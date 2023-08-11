package zlog

import (
	"context"
	"log/slog"
	"os"
	"runtime/pprof"

	"go.opentelemetry.io/otel/baggage"
)

var ExampleOpts = Options{
	OmitTime:   true,
	OmitSource: true,
}

func Example() {
	h := NewHandler(os.Stdout, &ExampleOpts)
	slog.New(h).With("a", "b").Info("test", "c", "d")

	// Output:
	// {"level":"INFO","msg":"test","a":"b","c":"d"}
}

func Example_pprof() {
	ctx := pprof.WithLabels(context.Background(), pprof.Labels("test_kind", "example"))
	pprof.SetGoroutineLabels(ctx)
	h := NewHandler(os.Stdout, &ExampleOpts)
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

func Example_baggage() {
	BaggageOpts := Options{
		OmitTime:   true,
		OmitSource: true,
		Baggage:    func(_ string) bool { return true },
	}
	b := must(baggage.New(
		must(baggage.NewMember("test_kind", "example")),
	))
	ctx := baggage.ContextWithBaggage(context.Background(), b)
	h := NewHandler(os.Stdout, &BaggageOpts)
	slog.New(h).InfoContext(ctx, "test")

	// Output:
	// {"level":"INFO","msg":"test","baggage":{"test_kind":"example"}}
}

func ExampleWithLevel() {
	opts := Options{
		OmitTime:   true,
		OmitSource: true,
	}
	levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	ctx := context.Background()
	h := NewHandler(os.Stdout, &opts)
	for _, l := range levels {
		ctx := WithLevel(ctx, l)
		for i, l := range levels {
			slog.New(h).LogAttrs(ctx, l, "normal log message", slog.Int("i", i))
		}
	}

	// Output:
	// {"level":"DEBUG","msg":"normal log message","i":0}
	// {"level":"INFO","msg":"normal log message","i":1}
	// {"level":"WARN","msg":"normal log message","i":2}
	// {"level":"ERROR","msg":"normal log message","i":3}
	// {"level":"INFO","msg":"normal log message","i":1}
	// {"level":"WARN","msg":"normal log message","i":2}
	// {"level":"ERROR","msg":"normal log message","i":3}
	// {"level":"WARN","msg":"normal log message","i":2}
	// {"level":"ERROR","msg":"normal log message","i":3}
	// {"level":"ERROR","msg":"normal log message","i":3}
}
