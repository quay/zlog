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
