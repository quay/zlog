//go:build !linux

package zlog

import (
	"io"
	"log/slog"
)

// TryJournald checks if the journal protocol should be used, and returns a
// handler and whether it should be used.
//
// On this platform, this function will always report false.
func tryJournald(_ io.Writer, _ *Options) (slog.Handler, bool) {
	return nil, false
}

// JournalStream reports whether the parent process has indicated the
// current process is connected to a journald stream on stderr.
//
// On this platform, this function will always report false.
func journalStream() bool { return false }
