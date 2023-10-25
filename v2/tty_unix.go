//go:build unix && !linux

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
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}
