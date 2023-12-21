package zlog

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"go.opentelemetry.io/otel/baggage"
)

// NeedEscape matches a string that needs to be escaped either into an ASCII or a percent-encoded representation.
var needEscape = regexp.MustCompile(`%(?:$|([0-9a-fA-F]?[^0-9a-fA-F]))|[^\x21\x23-\x2B\x2D-\x3A\x3C-\x5B\x5D-\x7E]`)

// PctEncode matches a string that requires some characters to be percent-encoded.
var pctEncode = regexp.MustCompile(`%(?:$|([0-9a-fA-F][^0-9a-fA-F])|[^0-9a-fA-F])| |"|,|;|\\`)

func escapeValue(v string) string {
	v = pctEncode.ReplaceAllStringFunc(v, func(m string) (r string) {
		for _, c := range m {
			switch c {
			case '%':
				r += "%25"
			case ' ':
				r += "%20"
			case '"':
				r += "%22"
			case ',':
				r += "%2C"
			case ';':
				r += "%3B"
			case '\\':
				r += "%5C"
			default:
				// Just copy to the return value.
				// This is (hopefully) just picking up the positions where percent-encoded nybbles would be.
				r += string(c)
			}
		}
		if len(m) == len(r) {
			panic(fmt.Sprintf("programmer error: pulled odd string %q", m))
		}
		return r
	})
	v = strconv.QuoteToASCII(v)
	return v[1 : len(v)-1]
}

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
		if needEscape.MatchString(v) {
			v = escapeValue(v)
		}
		m, err := baggage.NewMember(k, v)
		if err != nil {
			Warn(ctx).
				Err(err).
				Str("key", k).
				Str("value", v).
				Msg("failed to create baggage member")
			continue
		}
		n, err := b.SetMember(m)
		if err != nil {
			Warn(ctx).
				Err(err).
				Msg("failed to create baggage")
			continue
		}
		b = n
	}
	return baggage.ContextWithBaggage(ctx, b)
}
