package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clayton/harness-benchmark/internal/paths"
)

const SetupSchema = "hb.setup.v1"

// Setup is a named, user-owned recipe for direct rides. It is deliberately
// separate from study arms: saving a setup does not make a publishable claim.
type Setup struct {
	Schema    string  `json:"schema"`
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	Profile   Profile `json:"profile"`
	CreatedAt string  `json:"created_at"`
}

func SetupDir(l paths.Layout) string { return filepath.Join(l.DataDir, "setups") }

func setupPath(l paths.Layout, id string) string {
	return filepath.Join(SetupDir(l), id+".json")
}

func validSetupID(id string) bool {
	if id == "" || len(id) > 80 || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func SaveSetup(l paths.Layout, setup Setup) error {
	if !validSetupID(setup.ID) {
		return fmt.Errorf("invalid setup id %q; use letters, numbers, -, _, or .", setup.ID)
	}
	if setup.Schema == "" {
		setup.Schema = SetupSchema
	}
	if setup.Schema != SetupSchema {
		return fmt.Errorf("setup schema must be %s", SetupSchema)
	}
	if setup.Profile.Mode == "" {
		setup.Profile.Mode = "clean-baseline"
	}
	if setup.Profile.Mode != "personal" && setup.Profile.Mode != "clean-baseline" {
		return fmt.Errorf("invalid setup mode %q; use personal or clean-baseline", setup.Profile.Mode)
	}
	var err error
	setup.Profile, err = canonicalizeLocalSkills(setup.Profile)
	if err != nil {
		return err
	}
	if setup.CreatedAt == "" {
		setup.CreatedAt = Now()
	}
	raw, err := json.MarshalIndent(setup, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(SetupDir(l), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(setupPath(l, setup.ID), append(raw, '\n'), 0o600)
}

func LoadSetup(l paths.Layout, id string) (Setup, error) {
	if !validSetupID(id) {
		return Setup{}, fmt.Errorf("invalid setup id %q", id)
	}
	raw, err := os.ReadFile(setupPath(l, id))
	if err != nil {
		return Setup{}, fmt.Errorf("setup %q not found", id)
	}
	var setup Setup
	if err := json.Unmarshal(raw, &setup); err != nil {
		return Setup{}, fmt.Errorf("parse setup %s: %w", id, err)
	}
	if setup.ID != id || setup.Schema != SetupSchema {
		return Setup{}, fmt.Errorf("setup %q has invalid schema or id", id)
	}
	return setup, nil
}

func ListSetups(l paths.Layout) ([]Setup, error) {
	entries, err := os.ReadDir(SetupDir(l))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	setups := make([]Setup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		setup, err := LoadSetup(l, id)
		if err != nil {
			return nil, err
		}
		setups = append(setups, setup)
	}
	sort.Slice(setups, func(i, j int) bool { return setups[i].ID < setups[j].ID })
	return setups, nil
}

// FreezeLocalSkills copies each declared local skill directory into a run
// directory and records a content hash. The hash includes relative paths and
// file bytes, so a changed skill cannot silently reuse the old run input.
func FreezeLocalSkills(l paths.Layout, runID string, profile Profile) (Profile, error) {
	if len(profile.LocalSkills) == 0 {
		return profile, nil
	}
	if profile.Harness != "pi" {
		return Profile{}, fmt.Errorf("local skill directories are currently supported by the pi adapter only")
	}
	if err := ValidateRunID(runID); err != nil {
		return Profile{}, err
	}
	for index, source := range profile.LocalSkills {
		abs, err := canonicalLocalSkillDir(source)
		if err != nil {
			return Profile{}, err
		}
		name := safeFrozenSkillName(filepath.Base(abs), index+1)
		destRel := filepath.Join("skills", fmt.Sprintf("%02d-%s", index+1, name))
		dest := filepath.Join(l.RunDir(runID), destRel)
		digest, files, err := copySkillTree(abs, dest)
		if err != nil {
			return Profile{}, err
		}
		profile.Skills = append(profile.Skills, dest)
		profile.FrozenSkills = append(profile.FrozenSkills, FrozenSkill{Name: name, Path: filepath.ToSlash(destRel), Digest: digest, Files: files})
	}
	profile.LocalSkills = nil
	return profile, nil
}

// LocalSkillDigest returns a stable content hash for a local skill tree. It
// includes relative paths and file bytes, and never includes the source path.
// Study contracts use this before spending so a changed personal skill cannot
// silently alter a resumed arm.
func LocalSkillDigest(source string) (string, error) {
	abs, err := canonicalLocalSkillDir(source)
	if err != nil {
		return "", err
	}
	entries := []string{}
	err = filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(abs, path)
		if relErr != nil || rel == "." {
			return relErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("local skill contains unsupported symlink %q", rel)
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read local skill %q: %w", source, err)
	}
	sort.Strings(entries)
	hash := sha256.New()
	for _, rel := range entries {
		path := filepath.Join(abs, rel)
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", statErr
		}
		if info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("local skill contains unsupported file %q", rel)
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		hash.Write([]byte(filepath.ToSlash(rel)))
		hash.Write([]byte{0})
		hash.Write(raw)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// canonicalLocalSkillDir resolves both relative paths and a symlinked root.
// WalkDir deliberately rejects symlinks inside the tree, but a symlink used
// as the declared skill directory is a common way to keep skills elsewhere.
func canonicalLocalSkillDir(source string) (string, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve local skill %q: %w", source, err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve local skill %q: %w", source, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("resolve local skill %q: %w", source, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("local skill %q is not a directory", source)
	}
	return filepath.Clean(canonical), nil
}

func canonicalizeLocalSkills(profile Profile) (Profile, error) {
	for i, source := range profile.LocalSkills {
		canonical, err := canonicalLocalSkillDir(source)
		if err != nil {
			return Profile{}, err
		}
		profile.LocalSkills[i] = canonical
	}
	return profile, nil
}

func safeFrozenSkillName(raw string, index int) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := b.String()
	if len(name) > 128 {
		name = name[:128]
	}
	if name == "" || name == "." || name == ".." {
		return fmt.Sprintf("skill-%d", index)
	}
	return name
}

// CodexConfigFingerprint returns the digest and presence status of the
// personal Codex config without exposing its path or contents. Study contracts
// use this to detect a config change before creating another paid run.
func CodexConfigFingerprint() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home for personal Codex config: %w", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if os.IsNotExist(err) {
		return "", "missing", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read personal Codex config: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), "captured", nil
}

// FreezeCodexConfig captures the user's selected Codex configuration before a
// run is created. The execution environment remains isolated; only this
// frozen config is made visible to Codex in personal mode.
func FreezeCodexConfig(l paths.Layout, runID string, profile Profile) (Profile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Profile{}, fmt.Errorf("resolve home for personal Codex config: %w", err)
	}
	source := filepath.Join(home, ".codex", "config.toml")
	raw, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		profile.ConfigStatus = "missing"
		return profile, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read personal Codex config: %w", err)
	}
	path := filepath.Join(l.RunDir(runID), "config", "codex-config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Profile{}, err
	}
	if err := WriteFileAtomic(path, raw, 0o600); err != nil {
		return Profile{}, fmt.Errorf("freeze personal Codex config: %w", err)
	}
	digest := sha256.Sum256(raw)
	profile.ConfigPath = filepath.ToSlash(filepath.Join("config", "codex-config.toml"))
	profile.ConfigDigest = hex.EncodeToString(digest[:])
	profile.ConfigStatus = "captured"
	return profile, nil
}

func copySkillTree(source, dest string) (string, int, error) {
	entries := []string{}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("local skill contains unsupported symlink %q", rel)
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		return "", 0, fmt.Errorf("read local skill %q: %w", source, err)
	}
	sort.Strings(entries)
	hash := sha256.New()
	files := 0
	for _, rel := range entries {
		src := filepath.Join(source, rel)
		info, statErr := os.Stat(src)
		if statErr != nil {
			return "", 0, statErr
		}
		dst := filepath.Join(dest, rel)
		if info.IsDir() {
			if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
				return "", 0, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return "", 0, fmt.Errorf("local skill contains unsupported file %q", rel)
		}
		raw, readErr := os.ReadFile(src)
		if readErr != nil {
			return "", 0, readErr
		}
		hash.Write([]byte(filepath.ToSlash(rel)))
		hash.Write([]byte{0})
		hash.Write(raw)
		hash.Write([]byte{0})
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return "", 0, err
		}
		if err := os.WriteFile(dst, raw, info.Mode().Perm()); err != nil {
			return "", 0, err
		}
		files++
	}
	return hex.EncodeToString(hash.Sum(nil)), files, nil
}
