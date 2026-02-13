package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	l    *slog.Logger
	once sync.Once
	logW io.WriteCloser
)

type Config struct {
	Level      string // debug, info, warn, error
	Format     string // json or console
	OutputFile string // empty = stdout
}

func Init(cfg Config) error {
	var err error
	once.Do(func() {
		var lvl slog.Level
		if e := lvl.UnmarshalText([]byte(cfg.Level)); e != nil {
			lvl = slog.LevelInfo
		}

		var w io.Writer = os.Stdout
		if cfg.OutputFile != "" {
			dir := filepath.Dir(cfg.OutputFile)
			if dir != "." {
				if err = os.MkdirAll(dir, 0755); err != nil {
					return
				}
			}
			logW, err = os.OpenFile(cfg.OutputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return
			}
			w = logW
		}

		opts := &slog.HandlerOptions{Level: lvl}
		var h slog.Handler
		if cfg.Format == "json" {
			h = slog.NewJSONHandler(w, opts)
		} else {
			h = slog.NewTextHandler(w, opts)
		}

		l = slog.New(h)
	})
	return err
}

func must() *slog.Logger {
	if l == nil {
		panic("logger not initialised. Call logger.Init() first")
	}
	return l
}

// Sync closes any open log file.
func Sync() {
	if logW != nil {
		_ = logW.Close()
	}
}

// Package-level wrappers.
func Debug(msg string, args ...any) { must().Debug(msg, args...) }
func Info(msg string, args ...any)  { must().Info(msg, args...) }
func Warn(msg string, args ...any)  { must().Warn(msg, args...) }
func Error(msg string, args ...any) { must().Error(msg, args...) }

// With returns a child logger with the given attributes pre-attached.
func With(args ...any) *slog.Logger { return must().With(args...) }

// Reset tears down the logger and allows Init to be called again. Intended for tests.
func Reset() {
	Sync()
	l = nil
	logW = nil
	once = sync.Once{}
}
