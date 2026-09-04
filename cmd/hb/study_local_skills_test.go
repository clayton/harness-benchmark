package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/loop"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
)

func TestStudyLocalSkillDriftStopsBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	arm, err := profileToStudyArm(loop.Profile{ID: "skill", Mode: "personal", Harness: "pi", HarnessVersion: "test", Provider: "openrouter", Model: "model", LocalSkills: []string{dir}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := studycontract.Manifest{Visibility: "private", Arms: []studycontract.Arm{arm}}
	if err := verifyStudyLocalSkills(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyStudyLocalSkills(m); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("drift was not detected: %v", err)
	}
}
