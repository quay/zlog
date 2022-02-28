package zlog

import (
	"context"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/baggage"
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
	m, _ := baggage.NewMember("x", "5")
	b, _ := baggage.FromContext(ctx).SetMember(m)
	ctx = baggage.ContextWithBaggage(ctx, b)
	Log(ctx).Msg("5?")
	t.Log("should be 5")
	m, _ = baggage.NewMember("x", "6")
	b, _ = baggage.FromContext(ctx).SetMember(m)
	ctx = baggage.ContextWithBaggage(ctx, b)
	Log(ctx).Msg("6?")
	t.Log("should be 6")
}

func TestSub(t *testing.T) {
	ctx := Test(nil, t)
	t.Log("make sure subtests are intelligible")
	m, _ := baggage.NewMember("outer", "test")
	b, _ := baggage.FromContext(ctx).SetMember(m)
	ctx = baggage.ContextWithBaggage(ctx, b)
	t.Run("a", func(t *testing.T) {
		ctx := Test(ctx, t)
		m, _ := baggage.NewMember("inner", "a")
		b, _ := baggage.FromContext(ctx).SetMember(m)
		ctx = baggage.ContextWithBaggage(ctx, b)
		Log(ctx).Msg("hello")
	})
	t.Run("b", func(t *testing.T) {
		ctx := Test(ctx, t)
		m, _ := baggage.NewMember("inner", "b")
		b, _ := baggage.FromContext(ctx).SetMember(m)
		ctx = baggage.ContextWithBaggage(ctx, b)
		Log(ctx).Msg("hello")
	})
}

func TestContext(t *testing.T) {
	ctx := Test(context.Background(), t)
	ctx = ContextWithValues(ctx,
		"key1", "value1",
		"key2", "value2")
	Log(ctx).Msg("message")
	ctx = ContextWithValues(ctx,
		"key3", "value3",
		"key4", "value4")
	Log(ctx).Msg("message")
	ctx = ContextWithValues(ctx,
		"key1", "value5",
		"key2", "value6",
		"dropme")
	Log(ctx).Msg("message")
}

func TestContextWithBadChars(t *testing.T) {
	ctx := Test(context.Background(), t)
	ctx = ContextWithValues(ctx,
		"key1", "no bad news?#")
	Log(ctx).Msg("message")
	// Output:
	// {"key":"no%20bad%20news%3F%23","message":"message"}
}

func Example() {
	l := zerolog.New(os.Stdout)
	Set(&l)
	ctx := context.Background()
	m, _ := baggage.NewMember("key", "value1")
	b, _ := baggage.FromContext(ctx).SetMember(m)
	ctx = baggage.ContextWithBaggage(ctx, b)
	Log(ctx).Msg("message")
	m, _ = baggage.NewMember("key", "value2")
	b, _ = baggage.FromContext(ctx).SetMember(m)
	ctx = baggage.ContextWithBaggage(ctx, b)
	Log(ctx).Msg("message")
	// Output:
	// {"key":"value1","message":"message"}
	// {"key":"value2","message":"message"}
}

func ExampleContextWithValues() {
	l := zerolog.New(os.Stdout)
	Set(&l)
	ctx := context.Background()
	ctx = ContextWithValues(ctx, "key1", "value1")
	Log(ctx).Msg("message")
	ctx = ContextWithValues(ctx, "key1", "value2", "key2", "value1", "dropme")
	Log(ctx).Msg("message")
	// Output:
	// {"key1":"value1","message":"message"}
	// {"key2":"value1","key1":"value2","message":"message"}
}
