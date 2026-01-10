package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var (
	once       sync.Once
	defaultLog *slog.Logger
)

type contextKey string

const loggerKey contextKey = "logger"

func Init(ctx context.Context) {
	once.Do(func() {
		defaultLog = slog.New(&customHandler{out: os.Stdout})
	})
}

type customHandler struct {
	out io.Writer
}

func (h *customHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *customHandler) Handle(_ context.Context, r slog.Record) error {
	timestamp := r.Time.Format("2006-01-02 15:04:05")
	level := strings.ToUpper(r.Level.String())
	_, err := fmt.Fprintf(h.out, "[%s] [%s] %q\n", timestamp, level, r.Message)
	return err
}

func (h *customHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *customHandler) WithGroup(_ string) slog.Handler {
	return h
}

var contextStorage sync.Map

func WithContext(ctx context.Context) func() {
	gid := getGoroutineID()
	newCtx := context.WithValue(ctx, loggerKey, defaultLog)
	contextStorage.Store(gid, newCtx)

	return func() {
		contextStorage.Delete(gid)
	}
}

func getLogger() *slog.Logger {
	if ctx, ok := contextStorage.Load(getGoroutineID()); ok {
		if log, ok := ctx.(context.Context).Value(loggerKey).(*slog.Logger); ok {
			return log
		}
	}
	return defaultLog
}

type LogEntry struct {
	logger *slog.Logger
	level  slog.Level
}

func (e *LogEntry) Emitf(format string, args ...interface{}) {
	e.logger.Log(context.Background(), e.level, fmt.Sprintf(format, args...))
}

func Info() *LogEntry {
	return &LogEntry{logger: getLogger(), level: slog.LevelInfo}
}

func Error() *LogEntry {
	return &LogEntry{logger: getLogger(), level: slog.LevelError}
}

func Debug() *LogEntry {
	return &LogEntry{logger: getLogger(), level: slog.LevelDebug}
}

func Warn() *LogEntry {
	return &LogEntry{logger: getLogger(), level: slog.LevelWarn}
}

func Fatal() *LogEntry {
	return &LogEntry{logger: getLogger(), level: slog.LevelError}
}

func getGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, _ := strconv.ParseUint(idField, 10, 64)
	return id
}
