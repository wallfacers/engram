package mcpserver

import "testing"

func TestBuildLLMCallerUsesConfiguredProviderWithoutCallingNetwork(t *testing.T) {
	caller, err := BuildLLMCaller(ServerConfig{
		LLMProvider: "openai",
		LLMModel:    "test-model",
		LLMBaseURL:  "http://127.0.0.1:1/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if caller == nil {
		t.Fatal("configured provider did not produce an LLM caller")
	}

	offline, err := BuildLLMCaller(ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if offline != nil {
		t.Fatal("empty LLM configuration produced a caller")
	}
}
