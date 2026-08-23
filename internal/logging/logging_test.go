package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

type (
	testField string
	testValue string
)

const (
	testFieldComponent testField = "component"
	testFieldScope     testField = "scope"
	testFieldName      testField = "name"
	testFieldStatus    testField = "status"
	testFieldCount     testField = "count"
	testFieldEnabled   testField = "enabled"
	testValueDAP       testValue = "dap"
	testValueReady     testValue = "ready"
)

func (f testField) FieldName() string {
	return string(f)
}

func (v testValue) String() string {
	return string(v)
}

func TestLoggerEmitsTypedFieldsAndContext(t *testing.T) {
	var output bytes.Buffer
	base := zerolog.New(&output).Level(zerolog.DebugLevel)
	logger := New(base).
		With().
		Enum(testFieldComponent, testValueDAP).
		String(testFieldScope, "adapter").
		Logger()

	logger.Info().
		String(testFieldName, "session").
		Enum(testFieldStatus, testValueReady).
		Int(testFieldCount, 3).
		Bool(testFieldEnabled, true).
		Err(errors.New("failed")).
		Msg("test record")

	var record struct {
		Level     string `json:"level"`
		Message   string `json:"message"`
		Component string `json:"component"`
		Scope     string `json:"scope"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		Count     int    `json:"count"`
		Enabled   bool   `json:"enabled"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode record %q: %v", output.String(), err)
	}
	if record.Level != "info" || record.Message != "test record" ||
		record.Component != testValueDAP.String() || record.Scope != "adapter" ||
		record.Name != "session" || record.Status != testValueReady.String() ||
		record.Count != 3 || !record.Enabled || record.Error != "failed" {
		t.Fatalf("record = %#v", record)
	}
}

func TestLoggerPreservesLevelFiltering(t *testing.T) {
	var output bytes.Buffer
	logger := New(zerolog.New(&output).Level(zerolog.InfoLevel))

	logger.Debug().Msg("hidden")
	logger.Info().Msg("visible")

	var record struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode record %q: %v", output.String(), err)
	}
	if record.Level != "info" || record.Message != "visible" {
		t.Fatalf("record = %#v", record)
	}
}
