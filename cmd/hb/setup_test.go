package main

import (
	"testing"

	"github.com/clayton/harness-benchmark/internal/loop"
)

func TestSetupSaveStoresPersonalRecipe(t *testing.T) {
	t.Setenv("HB_DATA_DIR", t.TempDir())
	if err := run([]string{"setup", "save", "personal-pi", "--harness", "pi", "--provider", "openrouter", "--model", "openrouter/z-ai/glm", "--reasoning", "high", "--workflow", "plan-first"}); err != nil {
		t.Fatal(err)
	}
	setup, err := loop.LoadSetup(layout(), "personal-pi")
	if err != nil {
		t.Fatal(err)
	}
	if setup.Schema != loop.SetupSchema || setup.Profile.Mode != "personal" || setup.Profile.Model != "openrouter/z-ai/glm" || setup.Profile.Workflow != "plan-first" {
		t.Fatalf("setup=%+v", setup)
	}
}
