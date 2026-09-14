package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
	"gopkg.in/yaml.v3"
)

func TestPublicLocalStudyInitEmbedsPathFreeExecutableTask(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("HOME", t.TempDir())
	scenarioPath := filepath.Join(dir, "private-local-name.yaml")
	scenario := `id: public-demo
+type: bugfix
+title: Public demo
+description: Reproducible task
+language: go
+difficulty: easy
+tags: [go]
+repo:
+  url: https://github.com/example/project.git
+  base_ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
+  gold_ref: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
+prompt: Fix the regression.
+acceptance:
+  setup_commands: ["go mod download"]
+  test_commands: ["go test ./..."]
+  build_commands: []
+  fail_to_pass: ["regression"]
+  gold_files: ["private-gold.patch"]
+fetches:
+  - kind: cargo
+    lockfile: Cargo.lock
+`
	scenario = strings.ReplaceAll(scenario, "\n+", "\n")
	if err := os.WriteFile(scenarioPath, []byte(scenario), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private-gold.patch"), []byte("gold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "study.yaml")
	if err := initStudy([]string{"--question", "Which model?", "--scenario", scenarioPath, "--harness", "manual", "--model", "one", "--model", "two", "--out", out}); err != nil {
		t.Fatal(err)
	}
	manifest, err := studycontract.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != studycontract.SchemaV2 || len(manifest.Scenarios) != 1 {
		t.Fatalf("manifest=%+v", manifest)
	}
	frozen := manifest.Scenarios[0]
	if !strings.HasPrefix(frozen.ID, "local:") || strings.Contains(string(mustRead(t, out)), scenarioPath) {
		t.Fatalf("public contract leaked path: %s", mustRead(t, out))
	}
	hydrated, err := loadStudyLocalInputs(manifest)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveFrozenStudyScenario(paths.New(dir, dir), hydrated.Scenarios[0])
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != frozen.ID || resolved.Prompt != "Fix the regression." || resolved.Repo.GoldRef != strings.Repeat("b", 40) || resolved.Acceptance.TestCommands[0] != "go test ./..." || len(resolved.Acceptance.GoldFiles) != 1 || len(resolved.Fetches) != 1 {
		t.Fatalf("resolved=%+v", resolved)
	}
	if err := verifyStudyScenario(resolved, frozen); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(studyLocalInputsPath(manifest)); err != nil {
		t.Fatal(err)
	}
	standalone, err := resolveFrozenStudyScenario(paths.New(dir, dir), manifest.Scenarios[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(standalone.Fetches) != 1 || len(standalone.Acceptance.GoldFiles) != 1 || string(mustRead(t, filepath.Join(standalone.SourceDir, "private-gold.patch"))) != "gold\n" {
		t.Fatalf("standalone=%+v", standalone)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPublicStudyKeepsLocalSkillPathsOnlyInSecureSidecar(t *testing.T) {
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	skill := filepath.Join(root, "private-skill")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("public content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := loop.LocalSkillDigest(skill)
	if err != nil {
		t.Fatal(err)
	}
	m := studycontract.Manifest{Schema: studycontract.SchemaV2, ID: "local-skill-study", Question: "Which wins?", ComparisonMode: "controlled",
		Scenarios: []studycontract.Scenario{{ID: "rodeo:task@1", Digest: strings.Repeat("a", 64)}},
		Arms: []studycontract.Arm{
			{ID: "a", Mode: "personal", Harness: "pi", Version: "test", Model: "one", LocalSkills: []string{skill}, LocalSkillDigests: []string{digest}},
			{ID: "b", Mode: "personal", Harness: "pi", Version: "test", Model: "two"},
		}, VariedAxes: []string{"model", "skills"}, Repeats: 1, Seed: 1, JudgeProtocol: "default", WinRule: studycontract.WinRule, Budget: studycontract.Budget{MaxMinutes: 45}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := saveStudyLocalInputs(m); err != nil {
		t.Fatal(err)
	}
	public := publicStudyManifest(m)
	if len(public.Arms[0].LocalSkills) != 0 || len(public.Arms[0].LocalSkillDigests) != 1 {
		t.Fatalf("public arm=%+v", public.Arms[0])
	}
	hydrated, err := loadStudyLocalInputs(public)
	if err != nil {
		t.Fatal(err)
	}
	if hydrated.Arms[0].LocalSkills[0] != skill {
		t.Fatalf("hydrated arm=%+v", hydrated.Arms[0])
	}
	info, err := os.Stat(studyLocalInputsPath(m))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("sidecar mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestPromoteCompletedPrivateStudyCreatesVerifiedPublicContractWithoutRerun(t *testing.T) {
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("HOME", t.TempDir())
	scenarioPath := filepath.Join(root, "private-task.yaml")
	scenario := `id: private-task
+type: bugfix
+title: Private task
+description: Reproducible task
+language: go
+difficulty: hard
+tags: [go]
+repo:
+  url: https://github.com/example/project.git
+  base_ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
+  gold_ref: ""
+prompt: Fix the regression.
+acceptance:
+  setup_commands: []
+  test_commands: ["go test ./..."]
+  build_commands: []
+  fail_to_pass: ["regression"]
+  gold_files: ["private-gold.patch"]
+`
	scenario = strings.ReplaceAll(scenario, "\n+", "\n")
	if err := os.WriteFile(scenarioPath, []byte(scenario), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "private-gold.patch"), []byte("gold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := corpus.Resolve(layout().ScenariosDir(), "", scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := corpus.TrustDigest(resolved)
	if err != nil {
		t.Fatal(err)
	}
	source := studycontract.Manifest{Schema: studycontract.Schema, ID: "private-source", Visibility: "private", Question: "Which wins?", ComparisonMode: "controlled", Scenarios: []studycontract.Scenario{{ID: scenarioPath, Digest: digest}}, Arms: []studycontract.Arm{{ID: "a", Harness: "manual", Version: "human", Model: "one"}, {ID: "b", Harness: "manual", Version: "human", Model: "two"}}, VariedAxes: []string{"model"}, Repeats: 1, Seed: 1, JudgeProtocol: "scenario-default", WinRule: studycontract.WinRule, Budget: studycontract.Budget{MaxMinutes: 45}}
	if err := source.Validate(); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "private.yaml")
	raw, _ := yaml.Marshal(source)
	if err := os.WriteFile(sourcePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	state := studyState{Schema: "hb.study.state.v1", StudyID: source.ID, Digest: source.Digest(), Completed: []studyCell{{Arm: "a", Scenario: scenarioPath, Repeat: 1, RunID: "aaaaaaaaaaaa"}, {Arm: "b", Scenario: scenarioPath, Repeat: 1, RunID: "bbbbbbbbbbbb"}}}
	if err := saveStudyState(source, state); err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(root, "public.yaml")
	if err := promoteStudy(sourcePath, source, []string{"--out", publicPath}); err != nil {
		t.Fatal(err)
	}
	public, err := studycontract.Load(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if public.IsPrivate() || public.Schema != studycontract.SchemaV2 || public.ID != "private-source-public" || len(public.Scenarios[0].Task) == 0 || !strings.HasPrefix(public.Scenarios[0].ID, "local:") {
		t.Fatalf("public manifest=%+v", public)
	}
	hydrated, err := loadStudyLocalInputs(public)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := resolveFrozenStudyScenario(layout(), hydrated.Scenarios[0])
	if err != nil || len(executable.Acceptance.GoldFiles) != 1 {
		t.Fatalf("executable=%+v err=%v", executable, err)
	}
	publicState, err := loadStudyState(public)
	if err != nil || len(publicState.Completed) != 2 || publicState.Completed[0].RunID != "aaaaaaaaaaaa" || publicState.Completed[0].Scenario != public.Scenarios[0].ID {
		t.Fatalf("public state=%+v err=%v", publicState, err)
	}
	promotion, loadedSource, sourceState, err := loadStudyPromotion(public)
	if err != nil || promotion == nil || loadedSource.Digest() != source.Digest() {
		t.Fatalf("promotion=%+v source=%+v err=%v", promotion, loadedSource, err)
	}
	if err := validatePromotedStudyState(public, publicState, *promotion, loadedSource, sourceState); err != nil {
		t.Fatal(err)
	}
}

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
