package zlog

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/baggage"
)

// NeedEscape matches a string that needs to be escaped either into an ASCII or a percent-encoded representation.
var needEscape = regexp.MustCompile(`%(?:$|([0-9a-fA-F]?[^0-9a-fA-F]))|[^\x21\x23-\x2B\x2D-\x3A\x3C-\x5B\x5D-\x7E]|[\x80-\x{10FFFF}]`)

// PctEncode matches a string that requires some characters to be percent-encoded.
var pctEncode = regexp.MustCompile(`%(?:$|([0-9a-fA-F][^0-9a-fA-F])|[^0-9a-fA-F])|[^\x21\x23-\x2B\x2D-\x3A\x3C-\x5B\x5D-\x7E]+|[\x80-\x{10FFFF}]+`)

// EscapeOne is the set of 1-byte utf8 characters that should be percent encoded.
//
// This could be avoided if the [pctEncode] regexp was made robust enough to
// ignore correct hex escapes and only capture "lone" percent symbols.
var escapeOne = regexp.MustCompile(`[^\x21\x23-\x2B\x2D-\x3A\x3C-\x5B\x5D-\x7E]|%| |"|,|;|\\`)

func escapeValue(v string) string {
	const hexchar = `0123456789ABCDEF`
	var b strings.Builder
	b.Grow(4 * 3)
	return pctEncode.ReplaceAllStringFunc(v, func(v string) string {
		b.Reset()
		for _, c := range v {
			n := utf8.RuneLen(c)
			if n == 1 {
				c := byte(c)
				if escapeOne.Match([]byte{c}) {
					b.WriteRune('%')
					b.WriteByte(hexchar[c>>4])
					b.WriteByte(hexchar[c&15])
				} else {
					b.WriteByte(c)
				}
				continue
			}
			p := make([]byte, n)
			utf8.EncodeRune(p, c)
			for _, c := range p {
				b.WriteRune('%')
				b.WriteByte(hexchar[c>>4])
				b.WriteByte(hexchar[c&15])
			}
		}
		return b.String()
	})
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
