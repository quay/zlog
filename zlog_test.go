package zlog

import (
	"testing"

	"go.opentelemetry.io/otel/api/correlation"
	"go.opentelemetry.io/otel/api/kv"
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
	ctx = correlation.NewContext(ctx, kv.Int("x", 5))
	Log(ctx).Msg("5?")
	t.Log("should be 5")
	ctx = correlation.NewContext(ctx, kv.Int("x", 6))
	Log(ctx).Msg("6?")
	t.Log("should be 6")
}

func TestSub(t *testing.T) {
	ctx := Test(nil, t)
	t.Log("make sure subtests are intelligible")
	ctx = correlation.NewContext(ctx, kv.String("outer", "test"))
	t.Run("a", func(t *testing.T) {
		ctx := Test(ctx, t)
		ctx = correlation.NewContext(ctx, kv.String("inner", "a"))
		Log(ctx).Msg("hello")
	})
	t.Run("b", func(t *testing.T) {
		ctx := Test(ctx, t)
		ctx = correlation.NewContext(ctx, kv.String("inner", "b"))
		Log(ctx).Msg("hello")
	})
}
