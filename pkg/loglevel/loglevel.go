package loglevel

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Level string

const (
	Debug Level = "debug"
	Info  Level = "info"
	Warn  Level = "warn"
	Error Level = "error"
)

func (l Level) String() string {
	return string(l)
}

// ToSlog converts the Level to slog.Level for use with the standard library
func (l Level) ToSlog() slog.Level {
	switch l {
	case Debug:
		return slog.LevelDebug
	case Info:
		return slog.LevelInfo
	case Warn:
		return slog.LevelWarn
	case Error:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

