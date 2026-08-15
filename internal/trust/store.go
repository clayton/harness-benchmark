package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	Scenario  string `json:"scenario"`
	TrustedAt string `json:"trusted_at"`
}

type Store struct {
	Digests map[string]Entry `json:"digests"`
}

func Path(dataDir string) string { return filepath.Join(dataDir, "trust.json") }

func IsTrusted(dataDir, digest string) bool {
	store, err := load(dataDir)
	if err != nil {
		return false
	}
	_, ok := store.Digests[digest]
	return ok
}

func Remember(dataDir, digest, scenario string) error {
	if len(digest) != 64 {
		return fmt.Errorf("invalid scenario digest")
	}
	store, err := load(dataDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if store.Digests == nil {
		store.Digests = map[string]Entry{}
	}
	store.Digests[digest] = Entry{Scenario: scenario, TrustedAt: time.Now().UTC().Format(time.RFC3339)}
	dir := filepath.Dir(Path(dataDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".trust-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, Path(dataDir))
}

func load(dataDir string) (Store, error) {
	path := Path(dataDir)
	info, err := os.Lstat(path)
	if err != nil {
		return Store{Digests: map[string]Entry{}}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Store{}, fmt.Errorf("unsafe trust store: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Store{}, err
	}
	var store Store
	if err := json.Unmarshal(raw, &store); err != nil {
		return Store{}, err
	}
	if store.Digests == nil {
		store.Digests = map[string]Entry{}
	}
	return store, nil
}
