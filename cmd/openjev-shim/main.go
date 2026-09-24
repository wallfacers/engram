// Command openjev-shim is the local translation backend ("openjev") for the 051
// Jev relevance filter.
//
// The engine's filter/jev client speaks the Jev pointer protocol, and no
// reachable vendor implements it (api.deepseek.com/answers 404s), so the shim
// serves that wire shape on the inside edge and translates every call into one
// OpenAI-compatible chat.completions request against a local model on the
// outside edge: a single prompt carrying the query, the memories and the "noul"
// questions, and a single strict-JSON object of probabilities back.
//
// It is a translation layer, not Jev: the numbers are a local approximation of
// Jev's calibrated probabilities, so any eval claim that uses this backend is
// labeled "openjev" (data-model.md Backend enum) rather than "jev".
//
// It carries no credentials, reads none, and binds loopback only:
//
//	openjev-shim --port 8020
//	  OPENJEV_UPSTREAM_BASE_URL  OpenAI-compatible base URL (default http://127.0.0.1:8000/v1)
//	  OPENJEV_UPSTREAM_MODEL     upstream model revision (required)
//	  OPENJEV_MAX_QUESTIONS      per-request question bound (default 256, larger -> 413)
//
// The client's whole-call deadline is part of its own Config, so the caller that
// wires openjev must widen jev.Config.Deadline and jev.Config.PerRequestTimeout
// (a local model needs seconds, not the 1s default); the engine is untouched.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(args []string, getenv func(string) string) error {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return err
	}
	return serve(cfg)
}

// parseConfig resolves the flags and the environment into a server config. The
// model has no default on purpose: an openjev run must pin the model revision it
// measured, or the eval numbers are unattributable.
func parseConfig(args []string, getenv func(string) string) (shimConfig, error) {
	fs := flag.NewFlagSet("openjev-shim", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", defaultPort, "loopback port serving the pointer protocol")
	if err := fs.Parse(args); err != nil {
		return shimConfig{}, err
	}
	if len(fs.Args()) != 0 {
		return shimConfig{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *port < 1 || *port > 65535 {
		return shimConfig{}, fmt.Errorf("port must be in [1,65535], got %d", *port)
	}

	cfg := shimConfig{
		Port:            *port,
		UpstreamBaseURL: strings.TrimSpace(getenv("OPENJEV_UPSTREAM_BASE_URL")),
		UpstreamModel:   strings.TrimSpace(getenv("OPENJEV_UPSTREAM_MODEL")),
		MaxQuestions:    defaultMaxQuestions,
	}
	if raw := strings.TrimSpace(getenv("OPENJEV_MAX_QUESTIONS")); raw != "" {
		questions, err := strconv.Atoi(raw)
		if err != nil || questions <= 0 {
			return shimConfig{}, fmt.Errorf("OPENJEV_MAX_QUESTIONS must be a positive integer, got %q", raw)
		}
		cfg.MaxQuestions = questions
	}
	if err := cfg.applyDefaults(); err != nil {
		return shimConfig{}, err
	}
	return cfg, nil
}
