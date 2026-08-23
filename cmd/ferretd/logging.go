package main

import (
	"fmt"
	"io"

	"github.com/rs/zerolog"
)

type logLevel string

const (
	logLevelDebug logLevel = "debug"
	logLevelInfo  logLevel = "info"
	logLevelWarn  logLevel = "warn"
	logLevelError logLevel = "error"

	defaultLogLevel = logLevelInfo
)

func (l logLevel) String() string {
	return string(l)
}

func newLogger(output io.Writer, value logLevel) (*zerolog.Logger, error) {
	var level zerolog.Level
	switch value {
	case logLevelDebug:
		level = zerolog.DebugLevel
	case logLevelInfo:
		level = zerolog.InfoLevel
	case logLevelWarn:
		level = zerolog.WarnLevel
	case logLevelError:
		level = zerolog.ErrorLevel
	default:
		return nil, fmt.Errorf("log level must be debug, info, warn, or error")
	}

	logger := zerolog.New(zerolog.SyncWriter(output)).Level(level).With().Timestamp().Logger()

	return &logger, nil
}
