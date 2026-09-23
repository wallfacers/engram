package main

import (
	"strings"
	"testing"
)

func TestCanonicalJSONSortedKeysNoWhitespace(t *testing.T) {
	b, err := CanonicalJSON(map[string]any{"b": 1, "a": []any{true, nil, "x"}, "Z": "z"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Z":"z","a":[true,null,"x"],"b":1}`
	if string(b) != want {
		t.Fatalf("canonical mismatch:\n got %s\nwant %s", b, want)
	}
}

func TestCanonicalJSONStringEscaping(t *testing.T) {
	// Short escapes for \b \t \n \f \r; lowercase \u00xx for other control
	// characters; no HTML escaping; non-ASCII stays raw UTF-8.
	in := "a\"b\\c\nd\te\x01f\b\f\rh<i>&é漢"
	want := "{\"s\":\"a\\\"b\\\\c\\nd\\te\\u0001f\\b\\f\\rh<i>&é漢\"}"
	b, err := CanonicalJSON(map[string]any{"s": in})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Fatalf("escaping mismatch:\n got %s\nwant %s", b, want)
	}
}

func TestCanonicalJSONRejectsNonIntegralFloat(t *testing.T) {
	if _, err := CanonicalJSON(map[string]any{"x": 1.5}); err == nil {
		t.Fatal("expected float rejection")
	}
	b, err := CanonicalJSON(map[string]any{"x": float64(3)})
	if err != nil || string(b) != `{"x":3}` {
		t.Fatalf("integral float must render as integer: %s %v", b, err)
	}
}

func TestLFNormalizedDigest(t *testing.T) {
	a := LFNormalizedSHA256Bytes([]byte("line1\r\nline2\rline3\n"))
	b := LFNormalizedSHA256Bytes([]byte("line1\nline2\nline3\n"))
	if a != b {
		t.Fatalf("CRLF/CR normalization mismatch: %s != %s", a, b)
	}
	if len(a) != 64 || strings.ToLower(a) != a {
		t.Fatalf("digest must be lowercase 64-hex, got %s", a)
	}
	if _, err := LFNormalizedSHA256([]byte{0x00}); err == nil {
		t.Fatal("NUL must be rejected")
	}
	if _, err := LFNormalizedSHA256([]byte{0xff, 0xfe}); err == nil {
		t.Fatal("invalid UTF-8 must be rejected")
	}
}

func TestStrictParseClosedRejectsDefects(t *testing.T) {
	type inner struct {
		V int `json:"v"`
	}
	type outer struct {
		Name  string `json:"name"`
		Inner inner  `json:"inner"`
	}
	cases := []struct {
		name string
		in   string
	}{
		{"duplicate key top", `{"name":"a","name":"b"}`},
		{"duplicate key nested", `{"name":"a","inner":{"v":1,"v":2}}`},
		{"unknown top", `{"name":"a","inner":{"v":1},"extra":2}`},
		{"unknown nested", `{"name":"a","inner":{"v":1,"w":2}}`},
		{"float into int", `{"name":"a","inner":{"v":1.5}}`},
		{"trailing", `{"name":"a","inner":{"v":1}} {}`},
		{"nul", "{\"name\":\"a\x00\",\"inner\":{\"v\":1}}"},
	}
	for _, tc := range cases {
		var o outer
		if err := StrictParseClosed([]byte(tc.in), &o); err == nil {
			t.Errorf("%s: expected rejection", tc.name)
		}
	}
	// Valid nested input parses.
	var o outer
	in := `{"name":"a","inner":{"v":7}}`
	if err := StrictParseClosed([]byte(in), &o); err != nil {
		t.Fatalf("valid closed input rejected: %v", err)
	}
	if o.Inner.V != 7 {
		t.Fatalf("value not parsed: %+v", o)
	}
}
