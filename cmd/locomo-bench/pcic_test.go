package main

import (
	"reflect"
	"testing"
)

func TestPCICArmMechanismGatesSelector(t *testing.T) {
	baseline := optionsForArm(options{}, "hybrid+rerank")
	pcicArm := optionsForArm(options{}, "hybrid+rerank+pcic")

	if optionBool(t, baseline, "pcic") {
		t.Fatal("hybrid+rerank enabled PCIC selector")
	}
	if !optionBool(t, pcicArm, "pcic") {
		t.Fatal("hybrid+rerank+pcic did not enable PCIC selector")
	}
	if _, err := parseArm("hybrid+rerank+oracle"); err != nil {
		t.Fatalf("oracle arm was not recognized: %v", err)
	}

	pairedBaseline := optionsForRun(options{}, "hybrid+rerank", true)
	if optionBool(t, pairedBaseline, "pcic") {
		t.Fatal("paired hybrid+rerank baseline inherited PCIC selector")
	}
}

func optionBool(t *testing.T, opt options, field string) bool {
	t.Helper()
	v := reflect.ValueOf(opt).FieldByName(field)
	if !v.IsValid() {
		t.Fatalf("options.%s is missing", field)
	}
	if v.Kind() != reflect.Bool {
		t.Fatalf("options.%s has kind %s, want bool", field, v.Kind())
	}
	return v.Bool()
}
