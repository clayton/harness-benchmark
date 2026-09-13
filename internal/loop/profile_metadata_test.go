package loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestCreateRunSnapshotsV2ProfileMetadata(t *testing.T) {
	root := t.TempDir()
	layout := paths.New(root, root)
	task := map[string]any{"schema": "hb.task.v1", "id": "demo"}
	profile := Profile{
		ID: "arm-a", Harness: "manual", HarnessVersion: "human", Model: "demo", ModelVersion: "rev-1",
		PromptTreatment: map[string]any{"placement": "user_append"},
		Adapter:         map[string]any{"schema": "hb.adapter.v1", "id": "demo", "version": "1", "command": []any{"demo", "${prompt}"}},
		Assurance:       map[string]any{"harness": "declared"}, Task: task,
	}
	scenario := corpus.Scenario{ID: "local:demo", Title: "Demo", Prompt: "Fix it", Workspace: corpus.Workspace{Kind: "scaffold", Files: map[string]string{"README.md": "demo\n"}}}
	record, err := CreateRunWithProfile(layout, scenario, profile, false)
	if err != nil {
		t.Fatal(err)
	}
	if record.ModelVersion != "rev-1" {
		t.Fatalf("record=%+v", record)
	}
	raw, err := os.ReadFile(filepath.Join(layout.RunDir(record.ID), "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	config := snapshot["config"].(map[string]any)
	if config["model_version"] != "rev-1" || config["prompt_treatment"].(map[string]any)["placement"] != "user_append" || config["assurance"].(map[string]any)["harness"] != "declared" {
		t.Fatalf("config=%+v", config)
	}
	if snapshot["task"].(map[string]any)["id"] != "demo" || config["adapter"].(map[string]any)["id"] != "demo" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}
