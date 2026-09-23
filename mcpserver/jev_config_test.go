package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/wallfacers/engram/filter"
)

func TestJevConfigurationDefaultsStayOffline(t *testing.T) {
	config, err := LoadConfigWithEnv([]string{"--data-dir", t.TempDir()}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.JevBaseURL != "" || config.JevModel != "" || config.JevAPIKey != "" {
		t.Fatalf("offline config carries Jev endpoints: %#v", config)
	}
	if config.JevTheta != 0.5 || config.JevRelaxTheta != 0.35 || config.JevKShowMax != 12 {
		t.Fatalf("Jev threshold defaults = %v/%v/%v, want 0.5/0.35/12",
			config.JevTheta, config.JevRelaxTheta, config.JevKShowMax)
	}
	if !config.JevRelax {
		t.Fatal("relax stage must default to enabled (ENGRAM_JEV_RELAX unset)")
	}
	if config.JevWriteGate {
		t.Fatal("write gate must default to off (opt-in only)")
	}
	if config.SearchPool != 0 || config.SearchFilter != "" {
		t.Fatalf("server-side search defaults must stay unset, got pool=%d filter=%q", config.SearchPool, config.SearchFilter)
	}
	policy := config.SearchPolicy()
	if policy.Theta != 0.5 || policy.RelaxTheta != 0.35 || policy.KShowMax != 12 || policy.RelaxDisabled {
		t.Fatalf("default policy = %#v, want the shipped default with relax enabled", policy)
	}
}

func TestJevConfigurationReadsEnvironmentKnobs(t *testing.T) {
	env := map[string]string{
		"ENGRAM_DATA_DIR":        t.TempDir(),
		"ENGRAM_JEV_BASE_URL":    "http://jev.local:8000/",
		"ENGRAM_JEV_MODEL":       "jev-2026-09-r3",
		"ENGRAM_JEV_API_KEY":     "secret-key",
		"ENGRAM_JEV_THETA":       "0.6",
		"ENGRAM_JEV_RELAX_THETA": "0.4",
		"ENGRAM_JEV_KSHOW_MAX":   "9",
		"ENGRAM_JEV_RELAX":       "false",
		"ENGRAM_JEV_WRITE_GATE":  "1",
		"ENGRAM_SEARCH_POOL":     "150",
		"ENGRAM_FILTER":          "jev",
	}
	config, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.JevBaseURL != "http://jev.local:8000/" || config.JevModel != "jev-2026-09-r3" || config.JevAPIKey != "secret-key" {
		t.Fatalf("Jev endpoint knobs not read: %#v", config)
	}
	if config.JevTheta != 0.6 || config.JevRelaxTheta != 0.4 || config.JevKShowMax != 9 {
		t.Fatalf("Jev threshold knobs = %v/%v/%v", config.JevTheta, config.JevRelaxTheta, config.JevKShowMax)
	}
	if config.JevRelax || !config.JevWriteGate {
		t.Fatalf("boolean knobs = relax:%t write_gate:%t, want false/true", config.JevRelax, config.JevWriteGate)
	}
	if config.SearchPool != 150 || config.SearchFilter != filter.BackendJev {
		t.Fatalf("search defaults = pool:%d filter:%q", config.SearchPool, config.SearchFilter)
	}
	policy := config.SearchPolicy()
	if policy.Theta != 0.6 || policy.RelaxTheta != 0.4 || policy.KShowMax != 9 || !policy.RelaxDisabled {
		t.Fatalf("policy from knobs = %#v", policy)
	}
}

func TestJevConfigurationRejectsInvalidEnvironmentValues(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "ENGRAM_JEV_THETA", value: "abc"},
		{key: "ENGRAM_JEV_THETA", value: "0"},
		{key: "ENGRAM_JEV_THETA", value: "1"},
		{key: "ENGRAM_JEV_THETA", value: "1.5"},
		{key: "ENGRAM_JEV_RELAX_THETA", value: "0"},
		{key: "ENGRAM_JEV_RELAX_THETA", value: "2"},
		{key: "ENGRAM_JEV_KSHOW_MAX", value: "0"},
		{key: "ENGRAM_JEV_KSHOW_MAX", value: "many"},
		{key: "ENGRAM_JEV_RELAX", value: "sometimes"},
		{key: "ENGRAM_JEV_WRITE_GATE", value: "sometimes"},
		{key: "ENGRAM_SEARCH_POOL", value: "0"},
		{key: "ENGRAM_SEARCH_POOL", value: "lots"},
		{key: "ENGRAM_FILTER", value: "bogus"},
	}
	for _, test := range tests {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			env := map[string]string{"ENGRAM_DATA_DIR": t.TempDir(), test.key: test.value}
			_, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("invalid %s=%q error = %v", test.key, test.value, err)
			}
		})
	}
}

func TestJevRelaxThetaClampsToThetaAndPoolIsHardClamped(t *testing.T) {
	env := map[string]string{
		"ENGRAM_DATA_DIR":        t.TempDir(),
		"ENGRAM_JEV_THETA":       "0.7",
		"ENGRAM_JEV_RELAX_THETA": "0.9",
		"ENGRAM_SEARCH_POOL":     "1000",
	}
	config, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.SearchPool != maxCandidatePool {
		t.Fatalf("server-side pool = %d, want the honest-scale clamp %d", config.SearchPool, maxCandidatePool)
	}
	policy := config.SearchPolicy()
	if policy.RelaxTheta != policy.Theta || policy.Theta != 0.7 {
		t.Fatalf("RelaxTheta > Theta must clamp to Theta: %#v", policy)
	}
}

func TestBuildSearchFilterUnconfiguredReturnsUntypedNil(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "missing api key", env: map[string]string{"ENGRAM_JEV_BASE_URL": "http://jev.local", "ENGRAM_JEV_MODEL": "m"}},
		{name: "missing base url", env: map[string]string{"ENGRAM_JEV_MODEL": "m", "ENGRAM_JEV_API_KEY": "k"}},
		{name: "missing model", env: map[string]string{"ENGRAM_JEV_BASE_URL": "http://jev.local", "ENGRAM_JEV_API_KEY": "k"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := map[string]string{"ENGRAM_DATA_DIR": t.TempDir()}
			for key, value := range test.env {
				env[key] = value
			}
			config, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
			if err != nil {
				t.Fatal(err)
			}
			searchFilter, err := BuildSearchFilter(config)
			if err != nil {
				t.Fatal(err)
			}
			if searchFilter != nil {
				t.Fatalf("unconfigured Jev must collapse to an untyped nil, got %#v", searchFilter)
			}
			writeGate, err := BuildWriteGate(config)
			if err != nil {
				t.Fatal(err)
			}
			if writeGate != nil {
				t.Fatalf("unconfigured write gate must collapse to an untyped nil, got %#v", writeGate)
			}
			if !filter.IsNilRelevanceFilter(searchFilter) || !filter.IsNilWriteGate(writeGate) {
				t.Fatal("nil helpers must agree with the untyped nil")
			}
		})
	}
}

func TestConfiguredJevClientImplementsBothEngineInterfaces(t *testing.T) {
	env := map[string]string{
		"ENGRAM_DATA_DIR":     t.TempDir(),
		"ENGRAM_JEV_BASE_URL": "http://jev.local",
		"ENGRAM_JEV_MODEL":    "jev-2026-09-r3",
		"ENGRAM_JEV_API_KEY":  "k",
	}
	config, err := LoadConfigWithEnv(nil, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	searchFilter, err := BuildSearchFilter(config)
	if err != nil {
		t.Fatal(err)
	}
	if filter.IsNilRelevanceFilter(searchFilter) {
		t.Fatal("fully configured Jev must produce a usable filter")
	}
	writeGate, err := BuildWriteGate(config)
	if err != nil {
		t.Fatal(err)
	}
	if filter.IsNilWriteGate(writeGate) {
		t.Fatal("fully configured Jev must produce a usable write gate")
	}
}

func TestRegistryRejectsUnknownSearchFilterName(t *testing.T) {
	_, err := NewRegistry(context.Background(), RegistryConfig{
		DataDir:          t.TempDir(),
		SearchFilterName: "opaque",
	})
	if err == nil || !strings.Contains(err.Error(), "opaque") {
		t.Fatalf("unknown search filter name error = %v", err)
	}
}
