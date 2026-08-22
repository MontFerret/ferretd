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
		value string
		level zerolog.Level
	}{
		{value: "debug", level: zerolog.DebugLevel},
		{value: "info", level: zerolog.InfoLevel},
		{value: "warn", level: zerolog.WarnLevel},
		{value: "error", level: zerolog.ErrorLevel},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			logger, err := newLogger(&bytes.Buffer{}, test.value)
			if err != nil {
				t.Fatalf("newLogger: %v", err)
			}

			if logger.GetLevel() != test.level {
				t.Fatalf("logger level = %s, want %s", logger.GetLevel(), test.level)
			}
		})
	}
}

func TestNewLoggerWritesJSONLines(t *testing.T) {
	var output bytes.Buffer
	logger, err := newLogger(&output, "info")
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}

	logger.Info().Str("component", "test").Msg("started")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode diagnostic %q: %v", output.String(), err)
	}
	if record["level"] != "info" || record["message"] != "started" || record["component"] != "test" {
		t.Fatalf("diagnostic = %#v", record)
	}
	if _, ok := record["time"].(string); !ok {
		t.Fatalf("diagnostic timestamp = %#v", record["time"])
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
			if flag == nil || flag.DefValue != defaultLogLevel {
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
