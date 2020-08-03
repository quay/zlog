package zlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/api/correlation"
	"go.opentelemetry.io/otel/api/kv"
)

var (
	setup    sync.Once
	sink     logsink
	testName = kv.Key("zlog.testname")
)

// Test configures and wires up the global logger for testing.
//
// Log messages that do not use from a Context returned by this function will
// cause a panic.
func Test(t testing.TB) context.Context {
	t.Helper()
	setup.Do(sink.Setup)
	t.Cleanup(func() {
		t.Helper()
		t.Log("replaying application logs:")
		sink.Replay(t)
		sink.Remove(t)
	})
	sink.Create(t)
	return correlation.NewContext(context.Background(), testName.String(t.Name()))
}

// Logsink holds the files and does the routing for log messages.
type logsink struct {
	prefix string
	mu     sync.RWMutex
	fs     map[string]*os.File
}

// Setup configures the logsink and configures the global zerolog logger.
func (s *logsink) Setup() {
	s.fs = make(map[string]*os.File)
	f, err := ioutil.TempFile("", "zlog.")
	if err != nil {
		panic(err)
	}
	s.prefix = f.Name()
	if err := f.Close(); err != nil {
		panic(err)
	}
	if err := os.Remove(s.prefix); err != nil {
		panic(err)
	}

	// Set up caller information be default, because the testing package's line
	// information will be incorrect.
	zerolog.CallerMarshalFunc = func(file string, line int) string {
		return filepath.Base(file) + ":" + strconv.Itoa(line)
	}
	log.Logger = zerolog.New(s).With().Caller().Logger()
}

// Create initializes a new log stream.
func (s *logsink) Create(t testing.TB) {
	tn := t.Name()
	n := strings.ReplaceAll(tn, string(filepath.Separator), "_")
	n = s.prefix + "." + n
	f, err := os.Create(n)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Error(err)
	}
	s.mu.Lock()
	s.fs[tn] = f
	s.mu.Unlock()
}

// Replay pushes all messages for the test's log stream to the provided testing
// object.
func (s *logsink) Replay(t testing.TB) {
	t.Helper()
	n := t.Name()
	s.mu.RLock()
	f, err := os.Open(s.fs[n].Name())
	s.mu.RUnlock()
	if err != nil {
		t.Error(err)
		return
	}
	defer f.Close()
	l := bufio.NewScanner(f)
	for l.Scan() {
		t.Log(l.Text())
	}
	if err := l.Err(); err != nil {
		t.Error(err)
	}
}

// Remove tears down a log stream.
func (s *logsink) Remove(t testing.TB) {
	n := t.Name()
	s.mu.Lock()
	f := s.fs[n]
	delete(s.fs, n)
	s.mu.Unlock()
	os.Remove(f.Name())
	f.Close()
}

// Write routes writes to the correct stream.
func (s *logsink) Write(b []byte) (int, error) {
	var ev ev
	if err := json.Unmarshal(b, &ev); err != nil {
		return -1, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.fs[ev.Name]
	if !ok {
		panic(fmt.Sprintf("log write to unknown test %q:\n%s", ev.Name, string(b)))
	}
	return f.Write(b)
}

// Ev is used to pull the test name out of the zerolog Event.
type ev struct {
	Name string `json:"zlog.testname"`
}
