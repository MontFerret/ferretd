package main

import (
	"fmt"
	"io"

	"github.com/rs/zerolog"
)

const defaultLogLevel = "info"

func newLogger(output io.Writer, value string) (*zerolog.Logger, error) {
	var level zerolog.Level
	switch value {
	case "debug":
		level = zerolog.DebugLevel
	case "info":
		level = zerolog.InfoLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	default:
		return nil, fmt.Errorf("log level must be debug, info, warn, or error")
	}

	logger := zerolog.New(zerolog.SyncWriter(output)).Level(level).With().Timestamp().Logger()

	return &logger, nil
}
