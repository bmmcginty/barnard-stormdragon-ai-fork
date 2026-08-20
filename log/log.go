// Package log provides debug logging for the barnard audio pipeline.
// Logging is disabled by default and can be enabled via SetLogger.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Level represents logging severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "???"
	}
}

// Logger receives log messages. The default logger is a no-op.
type Logger interface {
	Log(level Level, format string, args ...interface{})
}

type loggerState struct {
	logger Logger
	level  Level
}

var logger atomic.Pointer[loggerState]

func init() {
	logger.Store(&loggerState{logger: &nopLogger{}, level: LevelError + 1})
}

type nopLogger struct{}

func (n *nopLogger) Log(level Level, format string, args ...interface{}) {}

// SetLogger sets the destination for log messages. Pass nil to disable.
func SetLogger(l Logger) {
	state := &loggerState{logger: l, level: LevelDebug}
	if l == nil {
		state.logger = &nopLogger{}
		state.level = LevelError + 1
	} else if writer, ok := l.(*WriterLogger); ok {
		state.level = writer.level
	}
	logger.Store(state)
}

// Enabled reports whether messages at level will be emitted. Callers should
// use it to avoid computing expensive log arguments when logging is disabled.
func Enabled(level Level) bool {
	return level >= logger.Load().level
}

// WriterLogger is a simple Logger that writes to an io.Writer.
type WriterLogger struct {
	mu    sync.Mutex
	w     io.Writer
	level Level
	buf   []byte
}

// NewWriterLogger creates a logger that writes to w, filtering below level.
func NewWriterLogger(w io.Writer, level Level) *WriterLogger {
	if w == nil {
		w = os.Stderr
	}
	return &WriterLogger{w: w, level: level}
}

func (wl *WriterLogger) Log(level Level, format string, args ...interface{}) {
	if level < wl.level {
		return
	}
	wl.mu.Lock()
	defer wl.mu.Unlock()
	now := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(wl.w, "%s [%-5s] %s\n", now, level.String(), msg)
}

func Debug(format string, args ...interface{}) {
	state := logger.Load()
	if LevelDebug >= state.level {
		state.logger.Log(LevelDebug, format, args...)
	}
}

func Info(format string, args ...interface{}) {
	state := logger.Load()
	if LevelInfo >= state.level {
		state.logger.Log(LevelInfo, format, args...)
	}
}

func Warn(format string, args ...interface{}) {
	state := logger.Load()
	if LevelWarn >= state.level {
		state.logger.Log(LevelWarn, format, args...)
	}
}

func Error(format string, args ...interface{}) {
	state := logger.Load()
	if LevelError >= state.level {
		state.logger.Log(LevelError, format, args...)
	}
}
