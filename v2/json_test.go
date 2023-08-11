package zlog

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStruct(t *testing.T) {
	type etc struct {
		A string
		B string
	}

	var buf bytes.Buffer
	ctx := context.Background()
	l := slog.New(NewHandler(&buf, nil))
	l.LogAttrs(ctx, slog.LevelInfo, "test message", slog.Any("etc", &etc{A: "C", B: "D"}))
	t.Logf("got:\n%s", buf.String())
	want := `"etc":{"A":"C","B":"D"}`
	if !strings.Contains(buf.String(), want) {
		t.Fail()
	}
}

func BenchmarkJSONAttrs(b *testing.B) {
	outfile, err := os.Create(filepath.Join(b.TempDir(), "log.out"))
	if err != nil {
		b.Fatal(err)
	}
	defer outfile.Close()
	for _, bench := range []struct {
		Name  string
		Out   io.Writer
		Attrs []slog.Attr
	}{
		{
			"NiceStrings",
			io.Discard,
			[]slog.Attr{
				slog.String("benchmark", "nice-strings"),
				slog.String("that means", "no escaping needed"),
			},
		},
		{
			"ToFile",
			outfile,
			[]slog.Attr{
				slog.String("benchmark", "to-file"),
				slog.String("still no", "escaping needed"),
			},
		},
	} {
		ctx := context.Background()
		b.Run(bench.Name, func(b *testing.B) {
			h := NewHandler(bench.Out, &Options{
				OmitSource: true,
				OmitTime:   true,
			})
			l := slog.New(h)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				l.LogAttrs(ctx, slog.LevelInfo, "perfectly normal log message", bench.Attrs...)
			}
		})
	}
}
