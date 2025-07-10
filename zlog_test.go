package zlog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/baggage"
)

func TestEscape(t *testing.T) {
	t.Run("NeedEscape", func(t *testing.T) {
		tt := []struct {
			In   string
			Want bool
		}{
			{"%25", false},
			{"%2C", false},
			{"%age", true},
			{"%", true},
			{"ꙮ", true},
			{"%", true},
			{" ", true},
			{`"`, true},
			{",", true},
			{";", true},
			{"\\", true},
			{"\n", true},
		}
		for _, tc := range tt {
			if got, want := needEscape.MatchString(tc.In), tc.Want; got != want {
				t.Errorf("needEscape.MatchString(%q): got: %v, want %v", tc.In, got, want)
			}
		}
	})
	t.Run("EscapeValue", func(t *testing.T) {
		tt := []struct {
			In   string
			Want string
		}{
			{`"🆒"`, `%22%F0%9F%86%92%22`},
			{`https://example.com?data=%70%62%73%73%77%6F%72%64&a=b`, `https://example.com?data=%70%62%73%73%77%6F%72%64&a=b`},
			{`20% done`, `20%25%20done`},
			{`%9Z`, `%259Z`},
			{`\n`, `%5Cn`},
			{`,`, `%2C`},
			{`;`, `%3B`},
			{"\n", `%0A`},
		}
		for _, tc := range tt {
			if got, want := escapeValue(tc.In), tc.Want; got != want {
				t.Error(cmp.Diff(got, want))
			}
		}
	})
}

func TestTestHarness(t *testing.T) {
	ctx := Test(context.TODO(), t)
	t.Log("🖳")
	Log(ctx).Msg("🖳")
	Log(ctx).Msg("🆒")
}

func TestDeduplication(t *testing.T) {
	ctx := Test(context.TODO(), t)
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
	ctx := Test(context.TODO(), t)
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
	var buf bytes.Buffer
	var got, want map[string]string
	l := zerolog.New(&buf)
	Set(&l)
	ctx := Test(context.Background(), t)

	ctx = ContextWithValues(ctx,
		"key1", "value1",
		"key2", "value2")
	Log(ctx).Msg("message")
	got = make(map[string]string)
	want = map[string]string{
		"zlog.testname": t.Name(),
		"key1":          "value1",
		"key2":          "value2",
		"message":       "message",
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Error(err)
	}
	if !cmp.Equal(got, want) {
		t.Error(cmp.Diff(got, want))
	}
	buf.Reset()

	ctx = ContextWithValues(ctx,
		"key3", "value3",
		"key4", "value4")
	Log(ctx).Msg("message")
	got = make(map[string]string)
	want = map[string]string{
		"zlog.testname": t.Name(),
		"key1":          "value1",
		"key2":          "value2",
		"key3":          "value3",
		"key4":          "value4",
		"message":       "message",
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Error(err)
	}
	if !cmp.Equal(got, want) {
		t.Error(cmp.Diff(got, want))
	}
	buf.Reset()

	ctx = ContextWithValues(ctx,
		"key1", "value5",
		"key2", "value6",
		"dropme")
	Log(ctx).Msg("message")
	got = make(map[string]string)
	want = map[string]string{
		"zlog.testname": t.Name(),
		"key1":          "value5",
		"key2":          "value6",
		"key3":          "value3",
		"key4":          "value4",
		"message":       "message",
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Error(err)
	}
	if !cmp.Equal(got, want) {
		t.Error(cmp.Diff(got, want))
	}
	buf.Reset()
}

func TestContextWithBadChars(t *testing.T) {
	ctx := Test(context.Background(), t)
	ctx = ContextWithValues(ctx,
		"key1", `no bad news",;\`,
		"key2", "all/fine.here")
	Log(ctx).Msg("message")
	// Output:
	// {"key1":"no%20bad%20news%22%2C%3B%5C","key2":"all/fine.here","message":"message"}
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
	// Can't capture the output because iteration order isn't guarenteed.
}
