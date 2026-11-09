package runtimes

import (
	"errors"
	"testing"
)

func TestWireEndpointNormalization(t *testing.T) {
	for _, test := range []struct{ endpoint, target string }{
		{"tcp://localhost:9000", "localhost:9000"},
		{"tcp://application.example:00080", "application.example:80"},
		{"tcp://127.0.0.1:1", "127.0.0.1:1"},
		{"tcp://[::1]:65535", "[::1]:65535"},
		{"tcp://[fe80::1%25en0]:9000", "[fe80::1%en0]:9000"},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
			catalog, err := NewCatalog(Config{Runtimes: []DefinitionConfig{{Name: "app", Provider: ProviderWire, Endpoint: test.endpoint}}})
			if err != nil {
				t.Fatal(err)
			}

			definition, err := catalog.Resolve("app")
			if err != nil || definition.Endpoint != test.endpoint || definition.target != test.target {
				t.Fatalf("definition = %+v, %v; target = %q", definition, err, test.target)
			}
		})
	}
}

func TestWireEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{
		"", " ", "localhost:9000", "tcp:localhost:9000", "http://localhost:9000", "dns:///localhost:9000",
		"tcp://:9000", "tcp://localhost", "tcp://localhost:", "tcp://localhost:0", "tcp://localhost:65536",
		"tcp://localhost:-1", "tcp://localhost:+1", "tcp://localhost:http", "tcp://localhost:1.0",
		"tcp://localhost:99999999999999999999999999999", "tcp://::1:9000", "tcp://[localhost]:9000",
		"tcp://[127.0.0.1]:9000", "tcp://[bad::ip]:9000", "tcp://user@localhost:9000", "tcp://user:secret@localhost:9000",
		"tcp://localhost:9000/", "tcp://localhost:9000/path", "tcp://localhost:9000?x=y", "tcp://localhost:9000?",
		"tcp://localhost:9000#fragment", "tcp://localhost:9000#", "tcp://local host:9000", " tcp://localhost:9000",
	} {
		t.Run(endpoint, func(t *testing.T) {
			catalog, err := NewCatalog(Config{Runtimes: []DefinitionConfig{{Name: "app", Provider: ProviderWire, Endpoint: endpoint}}})
			if catalog != nil || !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("NewCatalog(%q) = %v, %v", endpoint, catalog, err)
			}
		})
	}
}

func TestLoadInsecureBoolean(t *testing.T) {
	for _, test := range []struct {
		field string
		want  bool
		valid bool
	}{
		{"", false, true}, {`,"insecure":false`, false, true}, {`,"insecure":true`, true, true},
		{`,"insecure":null`, false, false}, {`,"insecure":"true"`, false, false},
		{`,"insecure":1`, false, false}, {`,"insecure":[]`, false, false}, {`,"insecure":{}`, false, false},
	} {
		t.Run(test.field, func(t *testing.T) {
			config, err := Load(t.Context(), writeConfig(t, `{"runtimes":[{"name":"app","provider":"wire","endpoint":"tcp://localhost:9000"`+test.field+`}]}`))
			if !test.valid {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("Load = %v", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			catalog, err := NewCatalog(config)
			if err != nil {
				t.Fatal(err)
			}

			definition, err := catalog.Resolve("app")
			if err != nil || definition.Insecure != test.want {
				t.Fatalf("definition = %+v, %v", definition, err)
			}
		})
	}
}
