package zlog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"runtime"
	"strconv"
	"time"
	"unicode/utf8"
)

// FormatterJSON is the set of formatting hooks for JSON output.
var formatterJSON = formatter[*stateJSON]{
	PprofKey:   "goroutine",
	BaggageKey: "baggage",

	Start: func(b *buffer, s *stateJSON) {
		b.WriteByte('{')
	},
	End: func(b *buffer, s *stateJSON) {
		if !s.wroteAttr {
			// If there are no attrs, the handler needs to make sure no empty
			// groups are written.
			for len(*b) > 0 {
				if b.Tail() != '{' {
					break
				}
				i := bytes.LastIndexByte(*b, ',')
				if i == -1 {
					i = 0
				}
				*b = (*b)[:i]
				s.groups--
			}
		}
		if b.Tail() == ',' {
			b.ReplaceTail('}')
		} else {
			b.WriteByte('}')
		}
		for i := s.groups; i > 0; i-- {
			b.WriteByte('}')
		}
		b.WriteByte('\n')
	},

	WriteSource: func(b *buffer, _ *stateJSON, f *runtime.Frame) {
		b.WriteByte('"')
		writeJSONString(b, slog.SourceKey)
		b.WriteString(`":"`)
		if fn := f.Func; fn != nil {
			writeJSONString(b, fn.Name())
		} else {
			writeJSONString(b, f.File)
			b.WriteByte(':')
			*b = strconv.AppendInt(*b, int64(f.Line), 10)
		}
		b.WriteString(`",`)
	},
	WriteLevel: func(b *buffer, s *stateJSON, l slog.Level) {
		b.WriteByte('"')
		writeJSONString(b, slog.LevelKey)
		b.WriteString(`":"`)
		b.WriteString(l.String())
		b.WriteString(`",`)
	},
	WriteMessage: func(b *buffer, s *stateJSON, m string) {
		b.WriteByte('"')
		writeJSONString(b, slog.MessageKey)
		b.WriteString(`":"`)
		writeJSONString(b, m)
		b.WriteString(`",`)
	},
	WriteTime: func(b *buffer, s *stateJSON, t time.Time) {
		b.WriteByte('"')
		writeJSONString(b, slog.TimeKey)
		b.WriteString(`":"`)
		*b = t.AppendFormat(*b, time.RFC3339Nano)
		b.WriteString(`",`)
	},

	AppendKey: func(b *buffer, s *stateJSON, k string) {
		s.wroteAttr = true
		b.WriteByte('"')
		writeJSONString(b, k)
		b.WriteString(`":`)
	},
	AppendString: func(b *buffer, s *stateJSON, v string) {
		b.WriteByte('"')
		writeJSONString(b, v)
		b.WriteString(`",`)
	},
	AppendBool: func(b *buffer, s *stateJSON, v bool) {
		*b = strconv.AppendBool(*b, v)
		b.WriteByte(',')
	},
	AppendInt64: func(b *buffer, s *stateJSON, v int64) {
		*b = strconv.AppendInt(*b, v, 10)
		b.WriteByte(',')
	},
	AppendUint64: func(b *buffer, s *stateJSON, v uint64) {
		*b = strconv.AppendUint(*b, v, 10)
		b.WriteByte(',')
	},
	AppendFloat64: func(b *buffer, s *stateJSON, v float64) {
		*b = strconv.AppendFloat(*b, v, 'g', -1, 64)
		b.WriteByte(',')
	},
	AppendTime: func(b *buffer, s *stateJSON, t time.Time) {
		// Lower-allocation trick copied from the slog source.
		b.WriteByte('"')
		*b = t.AppendFormat(*b, time.RFC3339Nano)
		b.WriteString(`",`)
	},
	AppendDuration: func(b *buffer, s *stateJSON, d time.Duration) {
		b.WriteByte('"')
		*b = append(*b, d.String()...)
		b.WriteString(`",`)
	},
	AppendAny: func(b *buffer, s *stateJSON, v any) error {
		// Copies slog's behavior because it seems sensible.
		m, isM := v.(json.Marshaler)
		err, isErr := v.(error)
		switch {
		case isErr && !isM:
			// Use the error's stringified version.
			b.WriteByte('"')
			writeJSONString(b, err.Error())
			b.WriteString(`",`)
		case isM:
			o, err := m.MarshalJSON()
			if err != nil {
				return err
			}
			b.Write(o)
			b.WriteByte(',')
		default:
			enc := json.NewEncoder(b)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(v); err != nil {
				return err
			}
			b.ReplaceTail(',')
		}
		return nil
	},

	PushGroup: func(b *buffer, s *stateJSON, g string) {
		s.groups++
		b.WriteByte('"')
		writeJSONString(b, g)
		b.WriteString(`":{`)
	},
	PopGroup: func(b *buffer, s *stateJSON) {
		s.groups--
		if b.Tail() == ',' {
			b.ReplaceTail('}')
		} else {
			b.WriteByte('}')
		}
		b.WriteByte(',')
	},
}

// StateJSON is the state needed to construct a JSON log record.
type stateJSON struct {
	groups    int
	wroteAttr bool
}

// Reset implements state.
func (s *stateJSON) Reset(g []string, _ *buffer) {
	s.groups = len(g)
	s.wroteAttr = false
}

// WriteJSONString escapes s for JSON and appends it to buf.
// It does not surround the string in quotation marks.
//
// Modified from encoding/json/encode.go:encodeState.string,
// with escapeHTML set to false.
func writeJSONString(b *buffer, s string) {
	start := 0
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if safeSet[c] {
				i++
				continue
			}
			if start < i {
				b.WriteString(s[start:i])
			}
			b.WriteByte('\\')
			switch c {
			case '\\', '"':
				b.WriteByte(c)
			case '\n':
				b.WriteByte('n')
			case '\r':
				b.WriteByte('r')
			case '\t':
				b.WriteByte('t')
			default:
				// This encodes bytes < 0x20 except for \t, \n and \r.
				b.WriteString(`u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if start < i {
				b.WriteString(s[start:i])
			}
			b.WriteString(`\ufffd`)
			i += size
			start = i
			continue
		}
		i += size
	}
	if start < len(s) {
		b.WriteString(s[start:])
	}
}

// Hex is the set of hex characters.
var hex = "0123456789abcdef"

// Copied from encoding/json/tables.go.
//
// safeSet holds the value true if the ASCII character with the given array
// position can be represented inside a JSON string without any further
// escaping.
//
// All values are true except for the ASCII control characters (0-31), the
// double quote ("), and the backslash character ("\").
//
// BUG(hank) The JSON encoding behavior for DEL (0x7F) seems wrong, but matches
// Go's [encoding/json] behavior.
var safeSet = [utf8.RuneSelf]bool{
	' ':      true,
	'!':      true,
	'"':      false,
	'#':      true,
	'$':      true,
	'%':      true,
	'&':      true,
	'\'':     true,
	'(':      true,
	')':      true,
	'*':      true,
	'+':      true,
	',':      true,
	'-':      true,
	'.':      true,
	'/':      true,
	'0':      true,
	'1':      true,
	'2':      true,
	'3':      true,
	'4':      true,
	'5':      true,
	'6':      true,
	'7':      true,
	'8':      true,
	'9':      true,
	':':      true,
	';':      true,
	'<':      true,
	'=':      true,
	'>':      true,
	'?':      true,
	'@':      true,
	'A':      true,
	'B':      true,
	'C':      true,
	'D':      true,
	'E':      true,
	'F':      true,
	'G':      true,
	'H':      true,
	'I':      true,
	'J':      true,
	'K':      true,
	'L':      true,
	'M':      true,
	'N':      true,
	'O':      true,
	'P':      true,
	'Q':      true,
	'R':      true,
	'S':      true,
	'T':      true,
	'U':      true,
	'V':      true,
	'W':      true,
	'X':      true,
	'Y':      true,
	'Z':      true,
	'[':      true,
	'\\':     false,
	']':      true,
	'^':      true,
	'_':      true,
	'`':      true,
	'a':      true,
	'b':      true,
	'c':      true,
	'd':      true,
	'e':      true,
	'f':      true,
	'g':      true,
	'h':      true,
	'i':      true,
	'j':      true,
	'k':      true,
	'l':      true,
	'm':      true,
	'n':      true,
	'o':      true,
	'p':      true,
	'q':      true,
	'r':      true,
	's':      true,
	't':      true,
	'u':      true,
	'v':      true,
	'w':      true,
	'x':      true,
	'y':      true,
	'z':      true,
	'{':      true,
	'|':      true,
	'}':      true,
	'~':      true,
	'\u007f': true,
}
