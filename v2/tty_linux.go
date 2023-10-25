package zlog

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func isatty(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := int(f.Fd())
	// This is how musl implements it. No muss, no fuss.
	_, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	return err == nil
}
