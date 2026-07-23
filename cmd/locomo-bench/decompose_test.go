package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecomposeFallsBackOnCallerFailure(t *testing.T) {
	question := "Where did Alice live before moving to Paris?"
	caller := modelCaller(func(context.Context, string, string) (string, error) {
		return "", errors.New("model unavailable")
	})

	got := decomposeQuery(context.Background(), caller, question, 4)
	want := []string{question}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decomposeQuery() = %#v, want fallback %#v", got, want)
	}
}

func TestDecomposeFallsBackOnTimeout(t *testing.T) {
	question := "When did Alice move to Paris?"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	caller := modelCaller(func(callCtx context.Context, _, _ string) (string, error) {
		<-callCtx.Done()
		return "", callCtx.Err()
	})

	got := decomposeQuery(ctx, caller, question, 4)
	want := []string{question}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decomposeQuery() = %#v, want timeout fallback %#v", got, want)
	}
}

func TestDecomposeFallsBackWhenResponseExceedsLimit(t *testing.T) {
	question := "Compare Alice's moves and jobs."
	caller := modelCaller(func(context.Context, string, string) (string, error) {
		return `["move one","move two","job one","job two","job three"]`, nil
	})

	got := decomposeQuery(context.Background(), caller, question, 4)
	want := []string{question}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decomposeQuery() = %#v, want over-limit fallback %#v", got, want)
	}
}

func TestDecomposeFallsBackWhenResponseIsHomogeneous(t *testing.T) {
	question := "What jobs did Alice have?"
	caller := modelCaller(func(context.Context, string, string) (string, error) {
		return `["Alice jobs", " alice   jobs ", "ALICE JOBS"]`, nil
	})

	got := decomposeQuery(context.Background(), caller, question, 4)
	want := []string{question}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decomposeQuery() = %#v, want homogeneous fallback %#v", got, want)
	}
}

func TestDecomposeReturnsBoundedQueriesWithOriginalFallback(t *testing.T) {
	question := "Where did Alice live before moving to Paris, and when did she move?"
	calls := 0
	caller := modelCaller(func(_ context.Context, system, user string) (string, error) {
		calls++
		if !strings.Contains(system, "JSON array") {
			t.Fatalf("system prompt does not require structured output: %q", system)
		}
		if !strings.Contains(user, question) || !strings.Contains(user, "4") {
			t.Fatalf("user prompt missing question or limit: %q", user)
		}
		return `["Where did Alice live before Paris?", "When did Alice move to Paris?"]`, nil
	})

	got := decomposeQuery(context.Background(), caller, question, 4)
	if calls != 1 {
		t.Fatalf("caller invoked %d times, want exactly once", calls)
	}
	if len(got) > 4 {
		t.Fatalf("decomposeQuery() returned %d queries, want at most 4: %#v", len(got), got)
	}
	if !containsString(got, question) {
		t.Fatalf("decomposeQuery() = %#v, want original question fallback %q", got, question)
	}
	if len(got) != 3 {
		t.Fatalf("decomposeQuery() = %#v, want two rewrites plus original", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
