package zlog

import (
	"testing"

	"go.opentelemetry.io/otel/api/correlation"
	"go.opentelemetry.io/otel/api/kv"
)

func TestTestHarness(t *testing.T) {
	ctx := Test(t)
	t.Log("🖳")
	Log(ctx).Msg("🖳")
	Log(ctx).Msg("🆒")
}

func TestDeduplication(t *testing.T) {
	ctx := Test(t)
	t.Log("make sure keys aren't repeated")
	ctx = correlation.NewContext(ctx, kv.Int("x", 5))
	Log(ctx).Msg("5?")
	ctx = correlation.NewContext(ctx, kv.Int("x", 6))
	Log(ctx).Msg("6?")
}
