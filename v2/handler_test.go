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
	"unicode"

	"github.com/google/go-cmp/cmp"
)

func TestHandler(t *testing.T) {
	var jsonMsgs []map[string]any
	var systemdMsgs []map[string]any

	t.Run("JSON", func(t *testing.T) {
		var buf bytes.Buffer
		h := NewHandler(&buf, nil)
		results := func() []map[string]any {
			var ms []map[string]any
			defer func() {
				jsonMsgs = ms
			}()
			for _, line := range bytes.Split(buf.Bytes(), []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var m map[string]any
				if err := json.Unmarshal(line, &m); err != nil {
					t.Errorf("in: %#q; error: %v", line, err)
				}
				ms = append(ms, m)
				t.Logf("msg: %+q", line)
			}
			return ms
		}
		if err := slogtest.TestHandler(h, results); err != nil {
			t.Error(err)
		}
		exerciseFormatter(t, h)
	})

	t.Run("Journald", func(t *testing.T) {
		emu := &emulator{}
		defer func() {
			systemdMsgs = emu.res
		}()

		h := &handler[*stateJournal]{
			out:  emu,
			opts: &Options{},
			fmt:  &formatterJournal,
			pool: getPool[*stateJournal](),
		}
		if err := slogtest.TestHandler(h, emu.Results); err != nil {
			t.Error(err)
		}
		exerciseFormatter(t, h)
	})

	t.Run("Prose", func(t *testing.T) {
		var buf bytes.Buffer
		h := proseHandler(&buf, &Options{})
		if err := slogtest.TestHandler(h, parseProseRecords(t, &buf)); err != nil {
			t.Error(err)
		}
		exerciseFormatter(t, h)
	})

	opts := cmp.Options{
		cmp.FilterPath(func(p cmp.Path) bool {
			s := p.Last()
			var k string
			switch idx := s.(type) {
			case cmp.MapIndex:
				k = idx.Key().String()
			default:
				return false
			}
			switch {
			case strings.Contains(k, "."):
			case !strings.ContainsFunc(k, unicode.IsLower):
			case k == "source":
			case k == "time":
			default:
				return false
			}
			return true
		}, cmp.Ignore()),
	}
	if !cmp.Equal(jsonMsgs, systemdMsgs, opts) {
		t.Log(cmp.Diff(jsonMsgs, systemdMsgs, opts))
	}
}

func exerciseFormatter(t *testing.T, h slog.Handler) {
	t.Helper()
	const n = 4 // https://xkcd.com/221/
	d := fnv.New64a()
	d.Write([]byte{0x00})
	v := plainstruct{
		Name: "plain",
		val:  any(json.RawMessage("{}")),
	}
	u, err := url.Parse("https://clairproject.org/")
	if err != nil {
		t.Fatal(err)
	}
	j := J([]int{5, 4, 3, 2, 1})
	ctx := WithLevel(context.Background(), slog.LevelDebug)
	log := slog.New(h)
	for _, l := range []slog.Level{slog.LevelError, slog.LevelWarn, slog.LevelInfo, slog.LevelDebug} {
		log.Log(ctx, l, "bool", "true", true, "false", false)
		log.Log(ctx, l, "int64", "0", int64(0))
		log.Log(ctx, l, "uint64", "0", uint64(0))
		log.Log(ctx, l, "float64", "0", float64(0))
		log.Log(ctx, l, "time", "0", time.Unix(0, 0))
		log.Log(ctx, l, "duration", "0", time.Duration(0))
		log.Log(ctx, l, "mulitline", "hello", strings.Repeat("\n", 16)+"goodbye")
		log.Log(ctx, l, "errors", "err", errors.New("err"))
		log.Log(ctx, l, "escaped characters", "string", "\\\"\t\r\n\x00\x80\x7f")
		log.Log(ctx, l, "byte slice", "b", make([]byte, 8))
		log.Log(ctx, l, "random struct", "v", &v)
		log.Log(ctx, l, "marshal json", "null", J(nil), "some", j)
		log.Log(ctx, l, "marshal text", "4", netip.IPv4Unspecified(), "6", netip.IPv6Unspecified())
		log.Log(ctx, l, "marshal binary", "fnv64a", d)
		log.Log(ctx, l, "stringer", "4", S(n))
		log.Log(ctx, l, "gostringer", "4", G(n))
		log.Log(ctx, l, "url", "link", u)
	}
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
	sync.Mutex
	res []map[string]any
}

func (e *emulator) Write(b []byte) (int, error) {
	cur := make(map[string]any)
	var key string
	var ct uint64
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
			var l string
			switch v.(string) {
			case "6":
				l = "INFO"
			default:
				return 0, fmt.Errorf("TODO: map priority %q", v)
			}
			cur[slog.LevelKey] = l
		case "MESSAGE":
			cur[slog.MessageKey] = v
		case "TIMESTAMP":
			cur[slog.TimeKey] = v
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
	return func() []map[string]any {
		var ms []map[string]any
		for _, line := range bytes.Split(buf.Bytes(), []byte{'\n'}) {
			// Validations:
			if len(line) == 0 {
				continue
			}
			t.Logf("msg: %+#q", line)
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
			m[slog.LevelKey] = l
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
