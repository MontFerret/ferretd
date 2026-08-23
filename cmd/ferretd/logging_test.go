package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewLoggerLevels(t *testing.T) {
	tests := []struct {
		value        logLevel
		zerologLevel zerolog.Level
	}{
		{value: logLevelDebug, zerologLevel: zerolog.DebugLevel},
		{value: logLevelInfo, zerologLevel: zerolog.InfoLevel},
		{value: logLevelWarn, zerologLevel: zerolog.WarnLevel},
		{value: logLevelError, zerologLevel: zerolog.ErrorLevel},
	}

	for _, test := range tests {
		t.Run(test.value.String(), func(t *testing.T) {
			logger, err := newLogger(&bytes.Buffer{}, test.value)
			if err != nil {
				t.Fatalf("newLogger: %v", err)
			}

			if logger.GetLevel() != test.zerologLevel {
				t.Fatalf("logger level = %s, want %s", logger.GetLevel(), test.zerologLevel)
			}
		})
	}
}

func TestNewLoggerWritesJSONLines(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, logLevelInfo)
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}

	logger.Info().Str("component", "test").Msg("started")

	var record struct {
		Level     string `json:"level"`
		Message   string `json:"message"`
		Component string `json:"component"`
		Time      string `json:"time"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode diagnostic %q: %v", output.String(), err)
	}
	if record.Level != logLevelInfo.String() || record.Message != "started" || record.Component != "test" {
		t.Fatalf("diagnostic = %#v", record)
	}
	if record.Time == "" {
		t.Fatal("diagnostic timestamp is empty")
	}
	if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("diagnostics are not one JSON record per line: %q", output.String())
	}
}

func TestLoggingCommandDefaults(t *testing.T) {
	for _, name := range []string{"serve", "dap"} {
		t.Run(name, func(t *testing.T) {
			command := newDAPCommand()
			if name == "serve" {
				command = newServeCommand("test")
			}

			flag := command.Flags().Lookup("log-level")
			if flag == nil || flag.DefValue != defaultLogLevel.String() {
				t.Fatalf("%s --log-level default = %#v, want %q", name, flag, defaultLogLevel)
			}
		})
	}
}

func TestLoggingCommandsRejectInvalidLogLevel(t *testing.T) {
	for _, command := range []string{"serve", "dap"} {
		t.Run(command, func(t *testing.T) {
			_, err := executeCommand(context.Background(), "test", command, "--log-level", "trace")
			if err == nil || !strings.Contains(err.Error(), "log level must be debug, info, warn, or error") {
				t.Fatalf("execute %s error = %v", command, err)
			}
		})
	}
}
