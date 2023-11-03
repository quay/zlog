package zlog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestHandler(t *testing.T) {
	var msgs struct {
		json, systemd, prose []map[string]any
	}

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		h := NewHandler(&buf, nil)
		results := func() []map[string]any {
			var ms []map[string]any
			for _, line := range bytes.Split(buf.Bytes(), []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var m map[string]any
				if err := json.Unmarshal(line, &m); err != nil {
					t.Errorf("in: %#q; error: %v", line, err)
				}
				ms = append(ms, m)
			}
			return ms
		}

		if err := slogtest.TestHandler(h, results); err != nil {
			t.Error(err)
		}
		msgs.json = results()
	})

	t.Run("Journald", func(t *testing.T) {
		emu := newEmulator(t)

		h := &handler[*stateJournal]{
			out:  emu,
			opts: &Options{},
			fmt:  &formatterJournal,
			pool: getPool[*stateJournal](),
		}
		if err := slogtest.TestHandler(h, emu.Results); err != nil {
			t.Error(err)
		}
		msgs.systemd = emu.Results()
	})

	t.Run("Prose", func(t *testing.T) {
		var buf bytes.Buffer
		h := proseHandler(&buf, &Options{})
		if err := slogtest.TestHandler(h, parseProseRecords(t, &buf)); err != nil {
			t.Error(err)
		}
		msgs.prose = parseProseRecords(t, &buf)()
	})

	// Test that our encodings are broadly similar.
	t.Run("Similar", func(t *testing.T) {
		opts := cmp.Options{
			cmpopts.AcyclicTransformer("KeyTransform", func(m map[string]any) map[string]any {
				_, isJournal := m["MESSAGE"]
				isProse := false
				for k := range m {
					if strings.Contains(k, ".") && !isJournal {
						isProse = true
						break
					}
				}
				switch {
				case isJournal:
					delete(m, "MESSAGE")
					delete(m, "PRIORITY")
					delete(m, "TIMESTAMP")
					if _, ok := m["CODE_FUNC"]; ok {
						m[slog.SourceKey] = m["CODE_FUNC"]
						delete(m, "CODE_FUNC")
						delete(m, "CODE_FILE")
						delete(m, "CODE_LINE")
					}
					m[slog.LevelKey] = m[slog.LevelKey].(slog.Level).String()
				case isProse:
				default: // JSON
					var err error
					if v, ok := m[slog.TimeKey]; ok {
						if s, ok := v.(string); ok {
							m[slog.TimeKey], err = time.Parse(time.RFC3339Nano, s)
							if err != nil {
								t.Error(err)
							}
						}
					}
				}

				// Flatten maps.
			Again:
				modified := false
				for k, v := range m {
					sm, ok := v.(map[string]any)
					if !ok {
						continue
					}
					for mk, mv := range sm {
						m[k+"."+mk] = mv
					}
					delete(m, k)
					modified = true
				}
				if modified {
					goto Again
				}

				return m
			}),
			cmp.Transformer("StringifyURL", func(u *url.URL) string { return u.String() }),
			cmp.Transformer("RewriteTimes", func(_ time.Time) string { return time.Unix(1_000_000_000, 0).Format(time.RFC3339) }),
			cmp.Transformer("StringifyBool", strconv.FormatBool),
			cmp.Transformer("StringifyFloat", func(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }),
		}
		if !cmp.Equal(msgs.json, msgs.systemd, opts) {
			t.Error(cmp.Diff(msgs.json, msgs.systemd, opts))
		}
		if !cmp.Equal(msgs.json, msgs.prose, opts) {
			t.Error(cmp.Diff(msgs.json, msgs.prose, opts))
		}
	})
}

var (
	exerciseCalls []struct {
		Msg   string
		Attrs []any
	}
	exerciseLevels = []slog.Level{slog.LevelError, slog.LevelWarn, slog.LevelInfo, slog.LevelDebug}
)

func init() {
	// Bunch of setup:
	const n = 4 // https://xkcd.com/221/
	d := fnv.New64a()
	d.Write([]byte{0x00})
	v := plainstruct{
		Name: "plain",
		val:  any(json.RawMessage("{}")),
	}
	u, err := url.Parse("https://clairproject.org/")
	if err != nil {
		panic(err)
	}
	j := J([]int{5, 4, 3, 2, 1})
	exerciseCalls = []struct {
		Msg   string
		Attrs []any
	}{
		{Msg: "bool", Attrs: []any{"true", true, "false", false}},
		{Msg: "int64", Attrs: []any{"0", int64(0)}},
		{Msg: "uint64", Attrs: []any{"0", uint64(0)}},
		{Msg: "float64", Attrs: []any{"0", float64(0)}},
		{Msg: "time", Attrs: []any{"0", time.Unix(0, 0)}},
		{Msg: "duration", Attrs: []any{"0", time.Duration(0)}},
		{Msg: "mulitline", Attrs: []any{"hello", strings.Repeat("\n", 16) + "goodbye"}},
		{Msg: "errors", Attrs: []any{"err", errors.New("err")}},
		{Msg: "escaped characters", Attrs: []any{"string", "\\\"\t\r\n\x00\x80\x7f"}},
		{Msg: "byte slice", Attrs: []any{"b", make([]byte, 8)}},
		{Msg: "random struct", Attrs: []any{"v", &v}},
		{Msg: "marshal json", Attrs: []any{"null", J(nil), "some", j}},
		{Msg: "marshal text", Attrs: []any{"4", netip.IPv4Unspecified(), "6", netip.IPv6Unspecified()}},
		{Msg: "marshal binary", Attrs: []any{"fnv64a", d}},
		{Msg: "stringer", Attrs: []any{"4", S(n)}},
		{Msg: "gostringer", Attrs: []any{"4", G(n)}},
		{Msg: "url", Attrs: []any{"link", u}},
	}
}

func exerciseFormatter(t *testing.T, h slog.Handler) {
	t.Helper()
	ctx := WithLevel(context.Background(), slog.LevelDebug)
	log := slog.New(h)
	for _, l := range exerciseLevels {
		for _, c := range exerciseCalls {
			log.Log(ctx, l, c.Msg, c.Attrs...)
		}
	}
}

// TestExercise prints out the same line run through every handler, for visual
// inspection.
//
// This could be a real test in the future, if we can figure out a good way to
// parse everything back to the same types/formats.
func TestExercise(t *testing.T) {
	var lines [][]string
	var desc []string
	for _, l := range exerciseLevels {
		for _, c := range exerciseCalls {
			desc = append(desc, fmt.Sprintf("%v %s", l, c.Msg))
		}
	}
	splitBuf := func(b []byte) {
		cur := make([]string, 0, 50)
		for _, l := range bytes.Split(b, []byte{'\n'}) {
			if len(l) == 0 {
				continue
			}
			cur = append(cur, printable(l))
		}
		lines = append(lines, cur)
	}

	// Generate all the lines:
	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		h := NewHandler(&buf, nil)
		exerciseFormatter(t, h)
		splitBuf(buf.Bytes())
	})
	t.Run("Journald", func(t *testing.T) {
		// The journal has harder framing, so the emulator has a hack to capture
		// the incoming messages.
		out := make([]string, 0, 50)
		emu := newEmulator(t)
		emu.Capture(&out)
		h := &handler[*stateJournal]{
			out:  emu,
			opts: &Options{},
			fmt:  &formatterJournal,
			pool: getPool[*stateJournal](),
		}
		exerciseFormatter(t, h)
		lines = append(lines, out)
	})
	t.Run("Prose", func(t *testing.T) {
		var buf bytes.Buffer
		h := proseHandler(&buf, &Options{})
		exerciseFormatter(t, h)
		splitBuf(buf.Bytes())
		t.Run("Color", func(t *testing.T) {
			var buf bytes.Buffer
			h := proseHandler(&buf, &Options{forceANSI: true})
			exerciseFormatter(t, h)
			splitBuf(buf.Bytes())
		})
	})

	// Print everything line-wise:
	for i, d := range desc {
		t.Logf("line %d: %s:", i+1, d)
		for j := range lines {
			t.Log(lines[j][i])
		}
	}
}

func printable(l []byte) string {
	// This is a rough port of the guts of strconv's quoting routing,
	// without the extra quoting needed to make a double-quoted string.
	var tmp [utf8.UTFMax]byte
	s := make([]byte, 0, len(l))
	for w := 0; len(l) > 0; l = l[w:] {
		r := rune(l[0])
		w = 1
		if r >= utf8.RuneSelf {
			r, w = utf8.DecodeRune(l)
		}
		if w == 1 && r == utf8.RuneError {
			s = append(s, `\x`...)
			s = append(s, hexChar[l[0]>>4])
			s = append(s, hexChar[l[0]&0xF])
			continue
		}
		print := strconv.IsPrint(r)
		switch {
		case print && w == 1:
			s = append(s, l[0])
		case print && w != 1:
			n := utf8.EncodeRune(tmp[:], r)
			s = append(s, tmp[:n]...)
		default:
			switch r {
			case '\a':
				s = append(s, `\a`...)
			case '\b':
				s = append(s, `\b`...)
			case '\f':
				s = append(s, `\f`...)
			case '\n':
				s = append(s, `\n`...)
			case '\r':
				s = append(s, `\r`...)
			case '\t':
				s = append(s, `\t`...)
			case '\v':
				s = append(s, `\v`...)
			default:
				switch {
				case r < ' ' || r == 0x7f:
					s = append(s, `\x`...)
					s = append(s, hexChar[byte(r)>>4])
					s = append(s, hexChar[byte(r)&0xF])
				case !utf8.ValidRune(r):
					r = 0xFFFD
					fallthrough
				case r < 0x10000:
					s = append(s, `\u`...)
					for x := 12; x >= 0; x -= 4 {
						s = append(s, hexChar[r>>uint(x)&0xF])
					}
				default:
					s = append(s, `\U`...)
					for x := 28; x >= 0; x -= 4 {
						s = append(s, hexChar[r>>uint(x)&0xF])
					}
				}
			}
		}
	}
	return string(s)
}

// Struct with no special marshaling implemented.
type plainstruct struct {
	Name string
	val  any
}

// Type implementing GoStringer, as done in the [fmt] tests.
type G int

func (g G) GoString() string {
	return fmt.Sprintf("GoString(%d)", int(g))
}

// Type implementing Stringer.
type S int

func (s S) String() string {
	return fmt.Sprintf("String(%d)", int(s))
}

// Type implementing [json.Marshaler].
type J []int

func (j J) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	b := []byte("[")
	for i, n := range j {
		if i != 0 {
			b = append(b, ',')
		}
		b = append(b, '[')
		b = strconv.AppendInt(b, int64(i), 10)
		b = append(b, ',')
		b = strconv.AppendInt(b, int64(n), 10)
		b = append(b, ']')
	}
	b = append(b, ']')
	return b, nil
}

// Emulator implements io.Writer and decodes the writes as a journald log
// message.
type emulator struct {
	t testing.TB
	sync.Mutex
	res []map[string]any
	cap *[]string
}

func newEmulator(t testing.TB) *emulator {
	emu := emulator{
		t: t,
	}
	return &emu
}

// Capture configures the emulator to append printable versions of every message
// to the passed slice.
func (e *emulator) Capture(msgs *[]string) {
	e.cap = msgs
}

// Write implements [io.Writer].
func (e *emulator) Write(b []byte) (int, error) {
	cur := make(map[string]any)
	var key string
	var ct uint64
	if e.cap != nil {
		// This will always copy the input slice, because the journal format
		// always contains newlines.
		s := printable(b)
		e.Lock()
		*e.cap = append(*e.cap, s)
		e.Unlock()
	}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		switch {
		case key != "" && ct == 0:
			// read Count:
			ct = binary.LittleEndian.Uint64(line)
			line = line[8:]
			cur[key] = make([]byte, 0, int(ct))
			cur[key] = append(cur[key].([]byte), line...)
			ct -= uint64(len(line))
			if ct != 0 {
				cur[key] = append(cur[key].([]byte), '\n')
				ct--
			}
			continue
		case key != "" && ct != 0:
			line = line[:min(len(line), int(ct))]
			cur[key] = append(cur[key].([]byte), line...)
			ct -= uint64(len(line))
			if ct == 0 {
				key = ""
			}
			continue
		default:
		}
		k, v, ok := bytes.Cut(line, []byte{'='})
		if ok {
			cur[string(k)] = string(v)
		} else {
			key = string(line)
		}
	}
	// Process the messages into the format expected by the test harness.
	for k, v := range cur {
		switch k {
		case "PRIORITY":
			var l slog.Level
			switch v.(string)[0] {
			case '7':
				l = SyslogDebug
			case '6':
				l = SyslogInfo
			case '5':
				l = SyslogNotice
			case '4':
				l = SyslogWarning
			case '3':
				l = SyslogError
			case '2':
				l = SyslogCritical
			case '1':
				l = SyslogAlert
			case '0':
				l = SyslogEmergency
			default:
				panic("unreachable")
			}
			cur[slog.LevelKey] = l
		case "MESSAGE":
			cur[slog.MessageKey] = v
		case "TIMESTAMP":
			micro, err := strconv.ParseInt(v.(string), 10, 64)
			if err != nil {
				return 0, err
			}
			cur[slog.TimeKey] = time.UnixMicro(micro)
		}
		if strings.Contains(k, ".") {
			m := cur
			ks := strings.Split(k, ".")
			for i, gk := range ks {
				if i == len(ks)-1 {
					m[gk] = v
					continue
				}
				var c map[string]any
				v, ok := m[gk]
				if !ok {
					c = make(map[string]any)
					m[gk] = c
				} else {
					c = v.(map[string]any)
				}
				m = c
			}
		}
	}
	e.Lock()
	e.res = append(e.res, cur)
	e.Unlock()
	return len(b), nil
}

func (e *emulator) Results() []map[string]any {
	e.Lock()
	defer e.Unlock()
	return e.res
}

func isUS(r rune) bool { return r == 0x1f }

func parseProseRecords(t *testing.T, buf *bytes.Buffer) func() []map[string]any {
	t.Helper()
	return func() []map[string]any {
		var ms []map[string]any
		for _, line := range bytes.Split(buf.Bytes(), []byte{'\n'}) {
			// Validations:
			if len(line) == 0 {
				continue
			}
			if line[len(line)-1] != 0x1e {
				t.Error("no ␞")
				continue
			}
			line = line[:len(line)-1]

			// Split into the fixed records and attrs:
			rec, attr, ok := bytes.Cut(line, []byte{0x1d})
			if !ok {
				t.Error("no ␝")
				continue
			}

			m := make(map[string]any)

			// Handle fixed records:
			recs := bytes.FieldsFunc(rec, isUS)
			var l slog.Level
			if err := l.UnmarshalText(bytes.TrimSpace(recs[0])); err != nil {
				t.Error(err)
			}
			m[slog.LevelKey] = l.String()
			recs = recs[1:]
			switch len(recs) {
			case 3:
				m[slog.SourceKey] = string(bytes.TrimSpace(recs[0]))
				recs = recs[1:]
				fallthrough
			case 2:
				s := string(bytes.TrimSpace(recs[0]))
				// Can be a path or a timestamp. Exploit the fact that we know
				// the time is in RFC3339 format and will not have path
				// separators.
				if strings.Contains(s, string(filepath.Separator)) {
					m[slog.SourceKey] = s
				} else {
					v, err := time.Parse(time.RFC3339, s)
					if err != nil {
						t.Error(err)
					}
					m[slog.TimeKey] = v
				}
				recs = recs[1:]
				fallthrough
			case 1:
				m[slog.MessageKey] = string(bytes.TrimSpace(recs[0]))
			}

			// Handle attrs:
			for _, f := range bytes.FieldsFunc(attr, isUS) {
				f = bytes.TrimSpace(f)
				if len(f) == 0 {
					continue
				}
				k, v, ok := strings.Cut(string(f), "=")
				if !ok {
					t.Errorf(`no "=": %+q`, string(f))
					continue
				}
				if v[0] == '"' {
					s, err := strconv.Unquote(v)
					if err != nil {
						t.Errorf("unable to unquote: %v", err)
						continue
					}
					v = s
				}
				// Reconstruct groups:
				ks := strings.Split(k, ".")
				cur := m
				for i, k := range ks {
					if i == len(ks)-1 {
						cur[k] = v
						continue
					}
					n, ok := cur[k]
					if !ok {
						n = make(map[string]any)
						cur[k] = n
					}
					cur = n.(map[string]any)
				}
			}
			ms = append(ms, m)
		}
		return ms
	}
}
