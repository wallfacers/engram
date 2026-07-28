package mcpserver

import (
	"context"
	"strings"
	"testing"
)

func TestCurationEnabledConfiguration(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want bool
	}{
		{name: "default disabled", want: false},
		{name: "environment enables", env: map[string]string{"ENGRAM_CURATION_ENABLED": "true"}, want: true},
		{
			name: "flag disables enabled environment",
			env:  map[string]string{"ENGRAM_CURATION_ENABLED": "true"},
			args: []string{"--curation-enabled=false"},
			want: false,
		},
		{
			name: "flag enables disabled environment",
			env:  map[string]string{"ENGRAM_CURATION_ENABLED": "false"},
			args: []string{"--curation-enabled=true"},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := map[string]string{"ENGRAM_DATA_DIR": t.TempDir()}
			for key, value := range test.env {
				env[key] = value
			}
			cfg, err := LoadConfigWithEnv(test.args, func(key string) string { return env[key] })
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CurationEnabled != test.want {
				t.Fatalf("CurationEnabled = %t, want %t", cfg.CurationEnabled, test.want)
			}
		})
	}
}

func TestCurationEnabledRejectsInvalidEnvironmentBoolean(t *testing.T) {
	env := map[string]string{
		"ENGRAM_DATA_DIR":         t.TempDir(),
		"ENGRAM_CURATION_ENABLED": "sometimes",
	}
	_, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
	if err == nil || !strings.Contains(err.Error(), "ENGRAM_CURATION_ENABLED") {
		t.Fatalf("invalid curation boolean error = %v", err)
	}
}

func TestRegistryRejectsEnabledCurationWithoutLLM(t *testing.T) {
	_, err := NewRegistry(context.Background(), RegistryConfig{
		DataDir:         t.TempDir(),
		CurationEnabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), "curation") || !strings.Contains(err.Error(), "LLM") {
		t.Fatalf("enabled curation without caller error = %v", err)
	}
}
