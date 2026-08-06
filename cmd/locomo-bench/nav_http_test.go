package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNavDecideCallerDisablesThinking: the navigation decide request must carry
// chat_template_kwargs.enable_thinking=false (fast pure-JSON tool calls) and a
// temperature of 0.
func TestNavDecideCallerDisablesThinking(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		got = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"tool\":\"search\",\"tool_args\":{\"query\":\"q\",\"k\":8}}"}}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`))
	}))
	defer ts.Close()

	call, err := newNavDecideCaller(navDecideConfig{BaseURL: ts.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	text, usage, err := call(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(text, `"tool":"search"`) {
		t.Fatalf("content = %q", text)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v", usage)
	}
	ctk, ok := got["chat_template_kwargs"].(map[string]any)
	if !ok || ctk["enable_thinking"] != false {
		t.Fatalf("chat_template_kwargs missing enable_thinking=false: %#v", got["chat_template_kwargs"])
	}
	if got["temperature"] != 0.0 {
		t.Fatalf("temperature = %v, want 0", got["temperature"])
	}
}

// TestNavDecideCallerErrorSurfaces: a non-200 response surfaces the HTTP
// status and a trimmed body so the caller can fail closed deterministically.
func TestNavDecideCallerErrorSurfaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad prompt"}}`))
	}))
	defer ts.Close()

	call, err := newNavDecideCaller(navDecideConfig{BaseURL: ts.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = call(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("want error on 400")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error must surface status: %v", err)
	}
}

// TestNavDecideCallerRejectsEmptyConfig: unconfigured caller must not silently
// construct a working instance.
func TestNavDecideCallerRejectsEmptyConfig(t *testing.T) {
	if _, err := newNavDecideCaller(navDecideConfig{}); err == nil {
		t.Fatal("want error on empty config")
	}
}
