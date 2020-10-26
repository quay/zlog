package zlog

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/label"
)

func TestTestHarness(t *testing.T) {
	ctx := Test(nil, t)
	t.Log("🖳")
	Log(ctx).Msg("🖳")
	Log(ctx).Msg("🆒")
}

func TestDeduplication(t *testing.T) {
	ctx := Test(nil, t)
	t.Log("make sure keys aren't repeated")
	ctx = otel.ContextWithBaggageValues(ctx, label.Int("x", 5))
	Log(ctx).Msg("5?")
	t.Log("should be 5")
	ctx = otel.ContextWithBaggageValues(ctx, label.Int("x", 6))
	Log(ctx).Msg("6?")
	t.Log("should be 6")
}

func TestSub(t *testing.T) {
	ctx := Test(nil, t)
	t.Log("make sure subtests are intelligible")
	ctx = otel.ContextWithBaggageValues(ctx, label.String("outer", "test"))
	t.Run("a", func(t *testing.T) {
		ctx := Test(ctx, t)
		ctx = otel.ContextWithBaggageValues(ctx, label.String("inner", "a"))
		Log(ctx).Msg("hello")
	})
	t.Run("b", func(t *testing.T) {
		ctx := Test(ctx, t)
		ctx = otel.ContextWithBaggageValues(ctx, label.String("inner", "b"))
		Log(ctx).Msg("hello")
	})
}

func Example() {
	ctx := context.Background()
	ctx = otel.ContextWithBaggageValues(ctx, label.String("key", "value1"))
	Log(ctx).Msg("message")
	ctx = otel.ContextWithBaggageValues(ctx, label.String("key", "value2"))
	Log(ctx).Msg("message")
}
