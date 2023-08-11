package zlog

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// FormatterJournal is the set of formatting hooks for journal output.
var formatterJournal = formatter[*stateJournal]{
	PprofKey:   "GOROUTINE",
	BaggageKey: "BAGGAGE",

	Start: func(b *buffer, s *stateJournal) {},
	End:   func(b *buffer, s *stateJournal) {},

	WriteSource: func(b *buffer, s *stateJournal, f *runtime.Frame) {
		if f := f.File; f != "" {
			b.WriteString(`CODE_FILE=`)
			journalString(b, f)
		}
		if l := f.Line; l != 0 {
			b.WriteString(`CODE_LINE=`)
			*b = strconv.AppendInt(*b, int64(f.Line), 10)
			b.WriteByte('\n')
		}
		if f.Func != nil {
			b.WriteString(`CODE_FUNC=`)
			journalString(b, f.Function)
		}
	},
	WriteLevel: func(b *buffer, s *stateJournal, l slog.Level) {
		b.WriteString(`PRIORITY=`)
		b.WriteByte(levelToPriority(l))
		b.WriteByte('\n')
	},
	WriteMessage: func(b *buffer, s *stateJournal, m string) {
		b.WriteString(`MESSAGE=`)
		journalString(b, m)
	},
	WriteTime: func(b *buffer, s *stateJournal, t time.Time) {
		// This is almost always unneeded, as the journal will timestamp
		// messages as they're received.
		b.WriteString(`TIMESTAMP=`)
		*b = strconv.AppendInt(*b, t.UnixMicro(), 10)
		b.WriteByte('\n')
	},

	AppendKey: func(b *buffer, s *stateJournal, k string) {
		if len(s.prefix) != 0 {
			b.Write(s.prefix)
			b.WriteByte('.')
		}
		b.WriteString(k)
		b.WriteByte('=')
	},
	AppendString: func(b *buffer, _ *stateJournal, v string) { journalString(b, v) },
	AppendBool: func(b *buffer, s *stateJournal, v bool) {
		*b = strconv.AppendBool(*b, v)
		b.WriteByte('\n')
	},
	AppendInt64: func(b *buffer, s *stateJournal, v int64) {
		*b = strconv.AppendInt(*b, v, 10)
		b.WriteByte('\n')
	},
	AppendUint64: func(b *buffer, s *stateJournal, v uint64) {
		*b = strconv.AppendUint(*b, v, 10)
		b.WriteByte('\n')
	},
	AppendFloat64: func(b *buffer, s *stateJournal, v float64) {
		*b = strconv.AppendFloat(*b, v, 'g', -1, 64)
		b.WriteByte('\n')
	},
	AppendTime: func(b *buffer, s *stateJournal, t time.Time) {
		*b = strconv.AppendInt(*b, t.UnixMicro(), 10)
		b.WriteByte('\n')
	},
	AppendDuration: func(b *buffer, s *stateJournal, d time.Duration) {
		b.WriteString(d.String())
		b.WriteByte('\n')
	},
	AppendAny: func(b *buffer, s *stateJournal, v any) error {
		err, isErr := v.(error)
		tm, canTM := v.(encoding.TextMarshaler)
		bm, canBM := v.(encoding.BinaryMarshaler)
		str, canStr := v.(fmt.Stringer)
		goStr, canGoStr := v.(fmt.GoStringer)
		bin, isBin := v.([]byte)
		switch {
		case isErr && !canTM:
			// Use the error's stringified version.
			journalString(b, err.Error())
		case canTM:
			m, err := tm.MarshalText()
			if err != nil {
				return err
			}
			journalString(b, string(m))
		case canBM:
			m, err := bm.MarshalBinary()
			if err != nil {
				return err
			}
			b.ReplaceTail('\n')
			*b = binary.LittleEndian.AppendUint64(*b, uint64(len(m)))
			b.Write(m)
			b.WriteByte('\n')
		case canStr:
			journalString(b, str.String())
		case canGoStr:
			journalString(b, goStr.GoString())
		case isBin:
			b.ReplaceTail('\n')
			*b = binary.LittleEndian.AppendUint64(*b, uint64(len(bin)))
			b.Write(bin)
			b.WriteByte('\n')
		default:
			i := len(*b)
			b.Write(make([]byte, 8))
			n, err := fmt.Fprintln(b, v)
			if err != nil {
				return err
			}
			n-- // Remove the added newline.
			binary.LittleEndian.PutUint64((*b)[i:i+8], uint64(n))
		}
		return nil
	},

	PushGroup: func(b *buffer, s *stateJournal, g string) { s.pushGroup(g) },
	PopGroup: func(b *buffer, s *stateJournal) {
		s.groups = s.groups[:len(s.groups)-1]
		i := bytes.LastIndexByte(s.prefix, '.')
		i--
		if i < 0 {
			i = 0
		}
		s.prefix = s.prefix[:i]
	},
}

// JournalString is a helper to emit the correct encoding for a journal value.
//
// This assumes that the tail byte in the buffer is '='.
func journalString(b *buffer, v string) {
	if strings.IndexByte(v, '\n') != -1 { // Do newline-ok style.
		b.ReplaceTail('\n')
		*b = binary.LittleEndian.AppendUint64(*b, uint64(len(v)))
	}
	b.WriteString(v)
	b.WriteByte('\n')
}

// LevelToPriority does a mapping of an slog.Level to a standard syslog
// priority.
func levelToPriority(l slog.Level) (p byte) {
	switch {
	case l <= SyslogDebug:
		p = '7'
	case l <= SyslogInfo:
		p = '6'
	case l <= SyslogNotice:
		p = '5'
	case l <= SyslogWarning:
		p = '4'
	case l <= SyslogError:
		p = '3'
	case l <= SyslogCritical:
		p = '2'
	case l <= SyslogAlert:
		p = '1'
	case l <= SyslogEmergency:
		p = '0'
	}
	return p
}

// StateJournal is the state needed to construct a journal-format log message.
type stateJournal struct {
	groups []string
	prefix []byte
	prefmt *buffer
}

// Reset implements state.
func (s *stateJournal) Reset(g []string, prefmt *buffer) {
	s.prefmt.Release()
	s.prefmt = prefmt.Clone()
	if s.groups != nil {
		s.groups = s.groups[:0]
	}
	if s.prefix != nil {
		s.prefix = s.prefix[:0]
	}
	for _, g := range g {
		s.pushGroup(g)
	}
}

// PushGroup adds a group to the formatter state.
func (s *stateJournal) pushGroup(g string) {
	s.groups = append(s.groups, g)
	if len(s.prefix) > 0 {
		s.prefix = append(s.prefix, '.')
	}
	s.prefix = append(s.prefix, g...)
}
