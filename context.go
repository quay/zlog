package zlog

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/baggage"
)

var esc = strings.NewReplacer(
	"%", "%25",
	" ", "%20",
	`"`, "%22",
	",", "%2C",
	";", "%3B",
	`\`, "%5C",
)

// ContextWithValues is a helper for the go.opentelemetry.io/otel/baggage v1
// API. It takes pairs of strings and adds them to the Context via the baggage
// package.
//
// Any trailing value is silently dropped.
func ContextWithValues(ctx context.Context, pairs ...string) context.Context {
	b := baggage.FromContext(ctx)
	pairs = pairs[:len(pairs)-len(pairs)%2]
	for i := 0; i < len(pairs); i = i + 2 {
		k, v := pairs[i], pairs[i+1]
		v = esc.Replace(v)
		m, err := baggage.NewMember(k, v)
		if err != nil {
			Warn(ctx).
				Err(err).
				Str("key", k).
				Str("value", v).
				Msg("failed to create baggage member")
			continue
		}
		b, err = b.SetMember(m)
		if err != nil {
			Warn(ctx).
				Err(err).
				Msg("failed to create baggage")
		}
	}
	return baggage.ContextWithBaggage(ctx, b)
}
