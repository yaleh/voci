package main

import (
	"testing"
)

func TestLoadCasesReturnsAtLeast15(t *testing.T) {
	cases, err := LoadCases("../../testdata/testcases.json")
	if err != nil {
		t.Fatalf("LoadCases failed: %v", err)
	}
	if len(cases) < 15 {
		t.Errorf("expected at least 15 cases, got %d", len(cases))
	}
}

func TestLoadCaseHasRequiredFields(t *testing.T) {
	cases, err := LoadCases("../../testdata/testcases.json")
	if err != nil {
		t.Fatalf("LoadCases failed: %v", err)
	}
	for _, c := range cases {
		if c.ID == "" {
			t.Errorf("case has empty ID: %+v", c)
		}
		if c.TTSInput == "" {
			t.Errorf("case %s has empty TTSInput", c.ID)
		}
		// ExpectedEntities is a slice (may be empty) — just verify the field exists (it always does in Go)
		_ = c.ExpectedEntities
	}
}

func TestLoadCasesAmbiguousCaseHasNoEntities(t *testing.T) {
	cases, err := LoadCases("../../testdata/testcases.json")
	if err != nil {
		t.Fatalf("LoadCases failed: %v", err)
	}
	for _, c := range cases {
		if c.ID == "sample-04" {
			if len(c.ExpectedEntities) != 0 {
				t.Errorf("expected sample-04 to have no entities, got %v", c.ExpectedEntities)
			}
			return
		}
	}
	t.Error("case sample-04 not found")
}
