//go:build linux

package zlog

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	run      = `systemd-run`
	logs     = `journalctl`
	magicEnv = `TEST_INNER_EXECUTION`
	idEnv    = `TEST_INNER_ID`
)

func TestJournald(t *testing.T) {
	if _, ok := os.LookupEnv(magicEnv); ok {
		emitJournaldLogs(t)
		return
	}

	for _, exe := range []string{run, logs} {
		switch _, err := exec.LookPath(exe); {
		case errors.Is(err, nil):
		case errors.Is(err, exec.ErrNotFound):
			t.Skipf("needed binary %q not found", exe)
		}
	}
	unitName := t.Name()
	idN, err := rand.Int(rand.Reader, new(big.Int).SetBit(new(big.Int), 128, 1))
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%x", idN)

	defer func() {
		if !t.Failed() {
			return
		}
		if err := exec.Command(`systemctl`, `--user`, `reset-failed`, unitName).Run(); err != nil {
			t.Log(err)
		}
	}()

	var buf bytes.Buffer
	cmd := exec.Command(run,
		`--user`,
		`--unit`, unitName,
		`--setenv`, magicEnv+`=1`,
		`--setenv`, fmt.Sprintf("%s=%s", idEnv, id),
		`--same-dir`,
		`--wait`,
	)
	// By re-using the command line, we transparently get correct coverage
	// stats. Go 1.21.0+ has a new cover format that the test coverage uses
	// under the hood.
	cmd.Args = append(cmd.Args, append(os.Args, `-test.run`, fmt.Sprintf("^%s$", unitName))...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	t.Logf("exec: %q", cmd.Args)
	if err := cmd.Run(); err != nil {
		t.Logf("output: %s", &buf)
		t.Fatal(err)
	}

	// If one is unlucky, it seems like the whole test duration can fit within
	// the buffer window, so explicitly ask for synchronization:
	buf.Reset()
	cmd = exec.Command(logs, `--user`, `--sync`)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if cmd.Run(); err != nil {
		t.Logf("output: %s", &buf)
		t.Logf(`"--sync" command: %v`, err)
	}

	buf.Reset()
	cmd = exec.Command(logs,
		`--user`,
		`--output`, `json`,
		`--all`,                  // Need to get messages larger than 4096 bytes.
		`USER_INVOCATION_ID=`+id, // Don't get the `go test` output.
		`_TRANSPORT=journal`,     // Don't get the "exercise" output.
	)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	t.Logf("exec: %q", cmd.Args)
	if err := cmd.Run(); err != nil {
		t.Logf("output: %s", &buf)
		t.Fatal(err)
	}
	dec := json.NewDecoder(&buf)

	var got journalMsg
	for _, want := range expected {
		if err := dec.Decode(&got); err != nil {
			t.Error(err)
		}
		if !cmp.Equal(got, want) {
			t.Error(cmp.Diff(got, want))
		}
	}
}

type journalMsg struct {
	Msg       string `json:"MESSAGE"`
	Transport string `json:"_TRANSPORT"`
}

var expected = []journalMsg{
	{
		Msg:       "test",
		Transport: "journal",
	},
	{
		Msg:       "embedded\nnewline",
		Transport: "journal",
	},
	{
		Msg:       "gigantic:\n" + strings.Repeat("⍼", 4096),
		Transport: "journal",
	},
}

// Only called from the process launched by systemd.
func emitJournaldLogs(t *testing.T) {
	t.Log("hello from inside systemd-run")
	h := NewHandler(os.Stderr, &Options{
		OmitTime:   true,
		OmitSource: true,
	})
	// This gets picked up in the parent test because of this field.
	log := slog.New(h).With("USER_INVOCATION_ID", os.Getenv(idEnv))
	for _, m := range expected {
		log.Info(m.Msg)
	}
	// These are just to make sure nothing barfs when talking to journald rather
	// than the emulator.
	exerciseFormatter(t, h)
	// Sweep the syslog priorities:
	pri := slog.New(h).With("TEST_PRIORITY", true)
	ctx := WithLevel(context.Background(), LevelEverything)
	for i := int64(-8); i < 21; i++ {
		pri.Log(ctx, slog.Level(i), "test", "SLOG_LEVEL", i)
	}
}
