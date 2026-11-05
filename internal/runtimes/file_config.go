package runtimes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type (
	fileConfig struct {
		DefaultRuntime configString    `json:"defaultRuntime"`
		Runtimes       json.RawMessage `json:"runtimes"`
	}

	fileDefinition struct {
		Name           configString `json:"name"`
		Label          configString `json:"label"`
		Provider       configString `json:"provider"`
		Endpoint       configString `json:"endpoint"`
		ConnectTimeout configString `json:"connectTimeout"`
	}

	// A separate presence bit distinguishes omission from an explicit empty string.
	configString struct {
		value string
		set   bool
	}
)

func (s *configString) UnmarshalJSON(data []byte) error {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	if value == nil {
		return errors.New("expected a string, got null")
	}

	s.value = *value
	s.set = true

	return nil
}

func (s configString) optional() *string {
	if !s.set {
		return nil
	}

	return &s.value
}

func (f fileConfig) config() (Config, error) {
	config := Config{DefaultRuntime: f.DefaultRuntime.optional()}
	if len(f.Runtimes) == 0 {
		return config, nil
	}

	data := bytes.TrimSpace(f.Runtimes)
	if len(data) == 0 || data[0] != '[' {
		return Config{}, errors.New("runtimes must be a JSON array")
	}

	var definitions []*fileDefinition
	if err := decodeConfigJSON(data, &definitions); err != nil {
		return Config{}, err
	}

	for index, definition := range definitions {
		if definition == nil {
			return Config{}, fmt.Errorf("runtime %d must be a JSON object", index)
		}

		config.Runtimes = append(config.Runtimes, DefinitionConfig{
			Name:           definition.Name.value,
			Label:          definition.Label.value,
			Provider:       Provider(definition.Provider.value),
			Endpoint:       definition.Endpoint.value,
			ConnectTimeout: definition.ConnectTimeout.optional(),
		})
	}

	return config, nil
}
