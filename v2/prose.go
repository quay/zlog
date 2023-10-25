package zlog

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultProseColors are the colors used when the "ZLOG_COLORS" environment
// variable isn't set.
const DefaultProseColors = `31:33:32:3:96:93::36::1;32:1;31:1;33:32:95:33:4:34:35:21:91`

// ProseHandler returns a handler emitting the "prose" format.
func proseHandler(w io.Writer, opts *Options) *handler[*stateJournal] {
	var p *ansiPrinter
	// Populate "p" if the configuration seems to support it.
	if opts.forceANSI || (len(os.Getenv("NO_COLOR")) != 0 && isatty(w)) {
		v := DefaultProseColors
		if z, ok := os.LookupEnv(`ZLOG_COLORS`); ok {
			// Scrub the string from the environment for disallowed runes.
			v = strings.Map(func(r rune) rune {
				if r < '0' || r > ';' {
					r = -1
				}
				return r
			}, z)
		}
		s := strings.Split(v, ":")
		// Ensure that the array is the correct size.
		if len(s) < printerSize {
			s = append(s, make([]string, printerSize-len(s))...)
		}
		p = (*ansiPrinter)((*[printerSize]string)(s))
	}

	f := formatter[*stateJournal]{
		PprofKey:   "goroutine",
		BaggageKey: "baggage",
		Start:      func(b *buffer, s *stateJournal) {},
		End: func(b *buffer, s *stateJournal) {
			b.Unwrite()
			b.Write([]byte("\x1e\n"))
		},
		WriteLevel: func(b *buffer, s *stateJournal, l slog.Level) {
			v := l.String()
			ct := 5 - len(v)
			switch {
			case l >= slog.LevelError:
				p.ErrorLevel(b, v)
			case l >= slog.LevelWarn:
				p.WarnLevel(b, v)
			case l >= slog.LevelInfo:
				p.InfoLevel(b, v)
			default:
				p.DebugLevel(b, v)
			}
			b.WriteString(strings.Repeat(" ", ct))
			emitUnitSep(b)
		},
		WriteSource: func(b *buffer, s *stateJournal, f *runtime.Frame) {
			defer emitUnitSep(b)
			defer p.Source(b)()
			b.WriteString(f.Function)
		},
		WriteTime: func(b *buffer, s *stateJournal, t time.Time) {
			p.Timestamp(b, t)
			emitUnitSep(b)
		},
		WriteMessage: func(b *buffer, s *stateJournal, msg string) {
			p.Message(b, msg)
			emitGroupSep(b)
		},
		AppendKey: func(b *buffer, s *stateJournal, k string) {
			defer b.WriteByte('=')
			defer p.Key(b)()
			if len(s.prefix) != 0 {
				b.Write(s.prefix)
				b.WriteByte('.')
			}
			b.WriteString(k)
		},
		AppendString: func(b *buffer, s *stateJournal, v string) {
			p.String(b, v)
			emitUnitSep(b)
		},
		AppendBool: func(b *buffer, s *stateJournal, v bool) {
			p.Bool(b, v)
			emitUnitSep(b)
		},
		AppendInt64: func(b *buffer, s *stateJournal, v int64) {
			defer emitUnitSep(b)
			defer p.Number(b)()
			*b = strconv.AppendInt(*b, v, 10)
		},
		AppendUint64: func(b *buffer, s *stateJournal, v uint64) {
			defer emitUnitSep(b)
			defer p.Number(b)()
			*b = strconv.AppendUint(*b, v, 10)
		},
		AppendFloat64: func(b *buffer, s *stateJournal, v float64) {
			defer emitUnitSep(b)
			defer p.Number(b)()
			*b = strconv.AppendFloat(*b, v, 'g', -1, 64)
		},
		AppendTime: func(b *buffer, s *stateJournal, t time.Time) {
			p.Time(b, t)
			emitUnitSep(b)
		},
		AppendDuration: func(b *buffer, s *stateJournal, d time.Duration) {
			p.Duration(b, d)
			emitUnitSep(b)
		},
		AppendAny: func(b *buffer, s *stateJournal, v any) (err error) {
			defer emitUnitSep(b)
			switch v := v.(type) {
			case *url.URL:
				p.URL(b, v)
			case error:
				p.Error(b, v)
			case encoding.TextMarshaler:
				var t []byte
				t, err = v.MarshalText()
				if err != nil {
					return err
				}
				p.Text(b, t)
			case fmt.Stringer:
				p.String(b, v.String())
			case fmt.GoStringer:
				p.GoStringer(b, v)
			case encoding.BinaryMarshaler:
				var t []byte
				t, err = v.MarshalBinary()
				if err != nil {
					return err
				}
				p.Base64(b, t)
			case []byte:
				p.Hex(b, v)
			case json.Marshaler:
				var t []byte
				t, err = v.MarshalJSON()
				if err != nil {
					return err
				}
				p.JSON(b, t)
			default:
				p.Reflect(b, v)
			}
			return nil
		},
		PushGroup: func(b *buffer, s *stateJournal, g string) { s.pushGroup(g) },
		PopGroup: func(b *buffer, s *stateJournal) {
			s.groups = s.groups[:len(s.groups)-1]
			i := bytes.LastIndexByte(s.prefix, '.')
			i--
			if i < 0 {
				i = 0
			}
			s.prefix = s.prefix[:i]
		},
	}

	return &handler[*stateJournal]{
		out:  &syncWriter{Writer: w},
		opts: opts,
		fmt:  &f,
		pool: getPool[*stateJournal](),
	}
}

// EmitUnitSep is used between output "columns".
//
// The output is "␟ ", which should render as " " in a terminal.
func emitUnitSep(b *buffer) { b.Write([]byte("\x1f ")) }

// EmitGroupSep is used after outputting the mandatory record components.
//
// The output is "␝ ", which should render as " " in a terminal.
func emitGroupSep(b *buffer) { b.Write([]byte("\x1d ")) }

// AnsiPrinter is a helper for decorating output with ANSI escape sequences.
//
// All methods are OK to call on a nil receiver, and will result in no escape
// sequences. If the method controls the printing, it will still happen.
type ansiPrinter [printerSize]string

// EmitEscape prints escape "i" and returns a function to reset the formatting.
func (p *ansiPrinter) emitEscape(b *buffer, i int) func() {
	if p == nil || p[i] == `` {
		return func() {}
	}
	b.WriteString("\x1b[")
	b.WriteString(p[i])
	b.WriteByte('m')
	return func() {
		b.WriteString("\x1b[m")
	}
}

// ErrorLevel prints "s" with the "ErrorLevel" formatting.
func (p *ansiPrinter) ErrorLevel(b *buffer, s string) {
	defer p.emitEscape(b, printErrorLevel)()
	b.WriteString(s)
}

// WarnLevel prints "s" with the "WarnLevel" formatting.
func (p *ansiPrinter) WarnLevel(b *buffer, s string) {
	defer p.emitEscape(b, printWarnLevel)()
	b.WriteString(s)
}

// InfoLevel prints "s" with the "InfoLevel" formatting.
func (p *ansiPrinter) InfoLevel(b *buffer, s string) {
	defer p.emitEscape(b, printInfoLevel)()
	b.WriteString(s)
}

// DebugLevel prints "s" with the "DebugLevel" formatting.
func (p *ansiPrinter) DebugLevel(b *buffer, s string) {
	defer p.emitEscape(b, printDebugLevel)()
	b.WriteString(s)
}

// Source emits the "Source" formatting.
func (p *ansiPrinter) Source(b *buffer) func() {
	return p.emitEscape(b, printSource)
}

// Timestamp prints "t" with the "Timestamp" formatting.
func (p *ansiPrinter) Timestamp(b *buffer, t time.Time) {
	defer p.emitEscape(b, printTimestamp)()
	*b = t.UTC().AppendFormat(*b, time.RFC3339)
}

// Time prints "t" with the "time.Time" formatting.
func (p *ansiPrinter) Time(b *buffer, t time.Time) {
	defer p.emitEscape(b, printTime)()
	*b = t.UTC().AppendFormat(*b, time.RFC3339)
}

// Message prints "s" with the "message" formatting.
func (p *ansiPrinter) Message(b *buffer, s string) {
	defer p.emitEscape(b, printMessage)()
	b.WriteString(s)
}

// Key emits the "key" formatting.
func (p *ansiPrinter) Key(b *buffer) func() {
	return p.emitEscape(b, printKey)
}

// String escapes and prints "s" with the "string" formatting.
func (p *ansiPrinter) String(b *buffer, s string) {
	defer p.emitEscape(b, printString)()
	*b = strconv.AppendQuote(*b, strings.ToValidUTF8(s, "\uFFFD"))
}

// Bool format and prints "v" with the relevant "boolTrue" or "boolFalse"
// formatting.
func (p *ansiPrinter) Bool(b *buffer, v bool) {
	i := printFalse
	if v {
		i = printTrue
	}
	defer p.emitEscape(b, i)()
	*b = strconv.AppendBool(*b, v)
}

// Number emits the "number" formatting.
func (p *ansiPrinter) Number(b *buffer) func() {
	return p.emitEscape(b, printNumber)
}

// Duration formats and prints "d" with the "time.Duration" formatting.
func (p *ansiPrinter) Duration(b *buffer, d time.Duration) {
	defer p.emitEscape(b, printDuration)()
	b.WriteString(d.String())
}

// Error formats and prints "err" with the "errorValue" formatting.
//
// If "err" is nil, "<nil>" is printed.
func (p *ansiPrinter) Error(b *buffer, err error) {
	defer p.emitEscape(b, printErrorVal)()
	s := "<nil>"
	if err != nil {
		s = err.Error()
	}
	b.WriteString(s)
}

// Text prints the preformatted bytes "t" with the "TextUnmarshaler" formatting.
func (p *ansiPrinter) Text(b *buffer, t []byte) {
	defer p.emitEscape(b, printTextUnmarshaler)()
	*b = append(*b, t...)
}

// GoStringer calls "GoString" and prints the result with the "GoStringer" formatting.
func (p *ansiPrinter) GoStringer(b *buffer, v fmt.GoStringer) {
	defer p.emitEscape(b, printGoString)()
	b.WriteString(v.GoString())
}

// Base64 base64-encodes "s" and prints it with the "binary" formatting.
func (p *ansiPrinter) Base64(b *buffer, s []byte) {
	defer p.emitEscape(b, printBinary)()
	b.WriteString(base64.RawStdEncoding.EncodeToString(s))
}

// Hex hex-encodes "s" and prints it with the "binary" formatting.
func (p *ansiPrinter) Hex(b *buffer, s []byte) {
	defer p.emitEscape(b, printBinary)()
	for _, c := range s {
		b.WriteByte(hexChar[c>>4])
		b.WriteByte(hexChar[c&0xF])
	}
}

// JSON prints the preformatted bytes "t" with the "JSON" formatting.
func (p *ansiPrinter) JSON(b *buffer, t []byte) {
	defer p.emitEscape(b, printJSON)()
	*b = append(*b, t...)
}

// Reflect uses the fmt package to print "v" with the "reflect" formatting.
func (p *ansiPrinter) Reflect(b *buffer, v any) {
	defer p.emitEscape(b, printReflect)()
	fmt.Fprint(b, v)
}

// URL prints prints "u" with OSC-8 formatting applied.
func (p *ansiPrinter) URL(b *buffer, u *url.URL) {
	s := u.String()
	if p == nil {
		b.WriteString(s)
		return
	}
	b.WriteString("\x1b]8;;")
	b.WriteString(s)
	b.WriteString("\x1b\\")
	b.WriteString(s)
	b.WriteString("\x1b]8;;\x1b\\")
}

// These are indexes into an array containing SGR parameters.
const (
	printErrorLevel = iota
	printWarnLevel
	printInfoLevel
	printDebugLevel
	printSource
	printTimestamp
	printMessage
	printKey
	printString
	printTrue
	printFalse
	printNumber
	printTime
	printDuration
	printErrorVal
	printTextUnmarshaler
	printGoString
	printBinary
	printJSON
	printReflect
	printerSize
)
