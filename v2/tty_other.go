//go:build !unix

package zlog

import "io"

func isatty(_ io.Writer) bool { return false }
