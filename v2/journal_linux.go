package zlog

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	// JournalAddr is the address of the journald socket. This is populated in
	// the [setupConn] function.
	journalAddr *net.UnixAddr
	// JournalConn is the socket to be used for sending messages to journald.
	// This is populated in the [setupConn] function.
	journalConn *net.UnixConn
	// MaxMsgSize is largest message that will fit in a datagram. Messages
	// larger than this are written to a memfd and the file descriptor passed.
	//
	// The initial sizing is conservative. The proper value is autodiscovered in
	// the [setupConn] function.
	maxMsgSize int64 = 4096

	// JournalStream reports whether the parent process has indicated the
	// current process is connected to a journald stream on stderr.
	journalStream = sync.OnceValue(func() bool {
		v, ok := os.LookupEnv("JOURNAL_STREAM")
		if !ok {
			return false
		}
		var dev, ino uint64
		if n, err := fmt.Sscanf(v, "%d:%d", &dev, &ino); n != 2 || err != nil {
			return false
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(os.Stderr.Fd()), &stat); err != nil {
			return false
		}
		return stat.Ino == ino && stat.Dev == dev
	})

	// SetupConn does setup for the per-process socket to journald.
	//
	// This function will panic if any of the needed files in /proc are not
	// accessible or if the setsockopt(2)-related calls fail.
	setupConn = sync.OnceFunc(func() {
		var err error
		journalAddr, err = net.ResolveUnixAddr("unixgram", "/run/systemd/journal/socket")
		if err != nil {
			panic(fmt.Errorf("programmer error: parsing static address: %w", err))
		}
		b, err := os.ReadFile("/proc/sys/net/core/wmem_default")
		if err != nil {
			panic(err)
		}
		def, err := strconv.ParseInt(string(b), 10, 64)
		if err == nil {
			maxMsgSize = def
		}

		b, err = os.ReadFile("/proc/sys/net/core/wmem_max")
		if err != nil {
			panic(err)
		}
		max, err := strconv.ParseInt(string(b), 10, 64)
		if err == nil {
			maxMsgSize = max
		}

		auto, err := net.ResolveUnixAddr("unixgram", "")
		if err != nil {
			panic(fmt.Errorf("programmer error: parsing static address: %w", err))
		}
		journalConn, err = net.ListenUnixgram("unixgram", auto)
		if err != nil {
			panic(err)
		}
		if def == max {
			// Don't need to adjust the send buffer size.
			return
		}

		sc, err := journalConn.SyscallConn()
		if err != nil {
			panic(err)
		}
		var ctlerr error
		if err := sc.Control(func(fd uintptr) {
			ctlerr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF, int(max))
		}); err != nil {
			panic(err)
		}
		if ctlerr != nil {
			panic(err)
		}
	})
)

// TryJournald checks if the journal protocol should be used, and returns a
// handler and whether it should be used.
func tryJournal(w io.Writer, opts *Options) (slog.Handler, bool) {
	if !journalStream() {
		return nil, false
	}
	if w != os.Stderr {
		return nil, false
	}
	setupConn()
	return &handler[*stateJournal]{
		opts: opts,
		fmt:  &formatterJournal,
		out:  journalWriter{},
		pool: getPool[*stateJournal](),
	}, true
}

// JournalWriter implements [io.Writer] by sending every [Write] call as a
// datagram or writing it to a memfd and sending the file descriptor.
type journalWriter struct{}

// MemfdSeals is the set of seals that need to be applied to memfds that are
// sent to journald.
const memfdSeals = unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE | unix.F_SEAL_SEAL

// Write implements [io.Writer].
func (journalWriter) Write(b []byte) (int, error) {
	var oob []byte
	if int64(len(b)) > maxMsgSize {
		fd, err := unix.MemfdCreate("journal-message", unix.MFD_ALLOW_SEALING)
		if err != nil {
			return 0, fmt.Errorf("zlog: journal write: unable to create memfd: %w", err)
		}
		defer unix.Close(fd)
		for len(b) > 0 {
			n, err := unix.Write(fd, b)
			b = b[n:]
			switch {
			case errors.Is(err, nil):
			case errors.Is(err, unix.EINTR):
			case errors.Is(err, unix.EAGAIN):
			default:
				return 0, fmt.Errorf("zlog: journal write: %w", err)
			}
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, memfdSeals); err != nil {
			return 0, fmt.Errorf("zlog: journal write: unable to seal memfd: %w", err)
		}
		oob = unix.UnixRights(fd)
		b = b[:0]
	}

	n, _, err := journalConn.WriteMsgUnix(b, oob, journalAddr)
	if oob != nil {
		return len(b), err
	}
	return n, err
}
