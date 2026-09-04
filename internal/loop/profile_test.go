package loop

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func TestSetupRoundTrip(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	setup := Setup{ID: "my-pi", Name: "My Pi", Profile: Profile{Mode: "personal", Harness: "pi", Provider: "openrouter", Model: "z-ai/glm", Reasoning: "high", Workflow: "plan-first", Skills: []string{"pkg@1.2.3"}}}
	if err := SaveSetup(l, setup); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSetup(l, setup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != SetupSchema || got.Profile.Mode != "personal" || got.Profile.Workflow != "plan-first" {
		t.Fatalf("setup=%+v", got)
	}
	list, err := ListSetups(l)
	if err != nil || len(list) != 1 || list[0].ID != setup.ID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestSaveSetupCanonicalizesRelativeSkillDir(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "skill")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("run it\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		t.Fatal(err)
	}
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	t.Chdir(from)
	setup := Setup{ID: "relative-skill", Profile: Profile{Mode: "personal", Harness: "pi", LocalSkills: []string{"../skill"}}}
	if err := SaveSetup(l, setup); err != nil {
		t.Fatal(err)
	}
	t.Chdir(to)
	got, err := LoadSetup(l, setup.ID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(skill)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Profile.LocalSkills) != 1 || got.Profile.LocalSkills[0] != want {
		t.Fatalf("saved profile=%+v want canonical skill=%s", got.Profile, want)
	}
}

func TestFreezeLocalSkillsCopiesAndHashesContent(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "my-skill")
	if err := os.MkdirAll(filepath.Join(skill, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("use tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "refs", "rules.md"), []byte("rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	l.OutDir = filepath.Join(root, "out")
	profile, err := FreezeLocalSkills(l, "aabbccddeeff", Profile{Mode: "personal", Harness: "pi", LocalSkills: []string{skill}})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.LocalSkills) != 0 || len(profile.FrozenSkills) != 1 || len(profile.Skills) != 1 {
		t.Fatalf("profile=%+v", profile)
	}
	frozen := profile.FrozenSkills[0]
	if filepath.IsAbs(frozen.Path) || frozen.Files != 2 || len(frozen.Digest) != 64 {
		t.Fatalf("frozen=%+v", frozen)
	}
	if _, err := os.Stat(filepath.Join(l.RunDir("aabbccddeeff"), filepath.FromSlash(frozen.Path), "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustJSON(t, profile)), skill) {
		t.Fatal("source path leaked into profile JSON")
	}
}

func TestFreezeLocalSkillsResolvesSymlinkedRoot(t *testing.T) {
	root := t.TempDir()
	realSkill := filepath.Join(root, "real-skill")
	link := filepath.Join(root, "linked-skill")
	if err := os.MkdirAll(realSkill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realSkill, "SKILL.md"), []byte("linked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realSkill, link); err != nil {
		t.Fatal(err)
	}
	l := paths.New(root, root)
	l.OutDir = filepath.Join(root, "out")
	profile, err := FreezeLocalSkills(l, "aabbccddeeff", Profile{Mode: "personal", Harness: "pi", LocalSkills: []string{link}})
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.FrozenSkills) != 1 || profile.FrozenSkills[0].Files != 1 {
		t.Fatalf("frozen=%+v", profile.FrozenSkills)
	}
	if _, err := os.Stat(filepath.Join(l.RunDir("aabbccddeeff"), filepath.FromSlash(profile.FrozenSkills[0].Path), "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenSkillNameIsSafeForPublishedIdentity(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "My Skill ☃")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := paths.New(root, root)
	l.OutDir = filepath.Join(root, "out")
	profile, err := FreezeLocalSkills(l, "aabbccddeeff", Profile{Mode: "personal", Harness: "pi", LocalSkills: []string{skill}})
	if err != nil {
		t.Fatal(err)
	}
	if profile.FrozenSkills[0].Name != "My-Skill--" {
		t.Fatalf("frozen name=%q", profile.FrozenSkills[0].Name)
	}
}

func TestFreezeLocalSkillsRejectsNonPiAndSymlink(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "skill")
	if err := os.Mkdir(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	l := paths.New(root, root)
	if _, err := FreezeLocalSkills(l, "aabbccddeeff", Profile{Harness: "codex", LocalSkills: []string{skill}}); err == nil {
		t.Fatal("non-Pi local skill was accepted")
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(skill, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := FreezeLocalSkills(l, "aabbccddeeff", Profile{Harness: "pi", LocalSkills: []string{skill}}); err == nil {
		t.Fatal("symlinked skill was accepted")
	}
}

func TestCreateRunSnapshotFreezesLocalSkillAndMode(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "skill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("do the task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := paths.New(root, root)
	l.DataDir = filepath.Join(root, "data")
	l.OutDir = filepath.Join(root, "out")
	rec, err := CreateRunWithProfile(l, corpus.Scenario{ID: "skill-demo", Prompt: "fix it", Workspace: corpus.Workspace{Kind: "scaffold", Files: map[string]string{"README.md": "demo\n"}}}, Profile{Mode: "personal", Harness: "pi", Provider: "openrouter", Model: "openrouter/z-ai/glm", LocalSkills: []string{skill}}, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(l.RunDir(rec.ID), "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	config := snapshot["config"].(map[string]any)
	if config["mode"] != "personal" {
		t.Fatalf("config=%v", config)
	}
	frozen := config["frozen_skills"].([]any)
	if len(frozen) != 1 || strings.Contains(string(raw), skill) {
		t.Fatalf("snapshot=%s", raw)
	}
	profile := rec.Metadata["profile"].(Profile)
	if err := verifyFrozenSkills(l, rec.ID, profile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.RunDir(rec.ID), filepath.FromSlash(profile.FrozenSkills[0].Path), "SKILL.md"), []byte("changed in frozen run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFrozenSkills(l, rec.ID, profile); err == nil || !strings.Contains(err.Error(), "changed in run") {
		t.Fatalf("frozen skill mutation was not detected: %v", err)
	}
}

func TestPersonalProfileAllowsRecordedWorkflowAndSkill(t *testing.T) {
	gotManual, err := NormalizeDirectProfile(Profile{Harness: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if gotManual.Mode != "personal" {
		t.Fatalf("manual default mode=%q", gotManual.Mode)
	}
	if _, err := NormalizeDirectProfile(Profile{Harness: "manual", Mode: "clean-baseline"}); err == nil || !strings.Contains(err.Error(), "unsupported study harness") {
		t.Fatalf("explicit manual clean baseline was accepted: %v", err)
	}
	got, err := NormalizeDirectProfile(Profile{Mode: "personal", Harness: "pi", Provider: "openrouter", Model: "openrouter/z-ai/glm", Workflow: "my-workflow", Skills: []string{"/tmp/skill"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "personal" || got.Workflow != "my-workflow" {
		t.Fatalf("profile=%+v", got)
	}
	if err := ValidateMeasuredProfile(Profile{Mode: "clean-baseline", Harness: "pi", Provider: "openrouter", Model: "openrouter/z-ai/glm", Workflow: "my-workflow"}); err == nil {
		t.Fatal("clean baseline accepted unenforced workflow")
	}
	if err := ValidateMeasuredProfile(Profile{Mode: "clean-baseline", Harness: "pi", Provider: "openrouter", Model: "openrouter/z-ai/glm", LocalSkills: []string{"/tmp/skill"}}); err == nil || !strings.Contains(err.Error(), "--mode personal") {
		t.Fatalf("clean baseline accepted local skill: %v", err)
	}
}

func TestNormalizeDirectProfileCanonicalizesLocalSkillDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	got, err := NormalizeDirectProfile(Profile{Mode: "personal", Harness: "pi", Provider: "openrouter", Model: "openrouter/z-ai/glm", LocalSkills: []string{"./skill"}})
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(root, "skill"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LocalSkills) != 1 || got.LocalSkills[0] != want || !filepath.IsAbs(got.LocalSkills[0]) {
		t.Fatalf("profile=%+v want=%s", got, want)
	}
	if _, err := NormalizeDirectProfile(Profile{Harness: "pi", Provider: "openrouter", Model: "openrouter/z-ai/glm", LocalSkills: []string{filepath.Join(root, "skill")}}); err == nil || !strings.Contains(err.Error(), "--mode personal") {
		t.Fatalf("clean baseline accepted local skill: %v", err)
	}
}

func TestCodexPersonalLaunchUsesSavedConfigMode(t *testing.T) {
	personal := HeadlessLaunchProfile(Profile{Mode: "personal", Harness: "codex", Model: "gpt-5"}, "fix it")
	clean := HeadlessLaunchProfile(Profile{Mode: "clean-baseline", Harness: "codex", Model: "gpt-5"}, "fix it")
	if strings.Contains(strings.Join(personal.Args, " "), "--ignore-user-config") {
		t.Fatalf("personal launch ignores user config: %+v", personal)
	}
	if !strings.Contains(strings.Join(clean.Args, " "), "--ignore-user-config") {
		t.Fatalf("clean launch does not ignore user config: %+v", clean)
	}
}

func TestFreezeCodexConfigRecordsDigestWithoutSourcePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("model = \"gpt-5\"\n")
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	l := paths.New(home, home)
	l.OutDir = filepath.Join(home, "out")
	profile, err := FreezeCodexConfig(l, "aabbccddeeff", Profile{Mode: "personal", Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ConfigStatus != "captured" || len(profile.ConfigDigest) != 64 || filepath.IsAbs(profile.ConfigPath) {
		t.Fatalf("profile=%+v", profile)
	}
	frozen, err := os.ReadFile(filepath.Join(l.RunDir("aabbccddeeff"), filepath.FromSlash(profile.ConfigPath)))
	if err != nil || string(frozen) != string(content) {
		t.Fatalf("frozen=%q err=%v", frozen, err)
	}
	if strings.Contains(string(mustJSON(t, profile)), home) {
		t.Fatal("source home path leaked into profile JSON")
	}
}

func TestPersonalCodexEnvironmentUsesFrozenConfig(t *testing.T) {
	root := t.TempDir()
	l := paths.New(root, root)
	l.OutDir = filepath.Join(root, "out")
	runID := "aabbccddeeff"
	configDir := filepath.Join(l.RunDir(runID), "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("model = \"gpt-5\"\n")
	if err := os.WriteFile(filepath.Join(configDir, "codex-config.toml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	rec := RunRecord{ID: runID, Harness: "codex", Metadata: map[string]any{"profile": Profile{Mode: "personal", ConfigPath: "config/codex-config.toml", ConfigDigest: fmt.Sprintf("%x", digest)}}}
	if _, err := isolatedHarnessEnv(l, rec); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(l.RunDir(runID), "harness-home", "codex", "config.toml"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("config=%q err=%v", got, err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
