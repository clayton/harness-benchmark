package loop

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

func openRooted(dir string) (*os.Root, error) {
	clean := filepath.Clean(dir)
	parent, base := filepath.Dir(clean), filepath.Base(clean)
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer parentRoot.Close()
	before, err := parentRoot.Lstat(base)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe root directory %q", dir)
	}
	root, err := parentRoot.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, fmt.Errorf("root directory changed while opening %q", dir)
	}
	return root, nil
}

func readRooted(dir, name string) ([]byte, error) {
	root, err := openRooted(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.FromSlash(name))
}

func writeRooted(dir, name string, data []byte, mode os.FileMode) error {
	root, err := openRooted(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	name = filepath.FromSlash(name)
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.Join(parent, ".hb-"+hex.EncodeToString(nonce[:]))
	file, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	cleanup := func() { _ = root.Remove(tmp) }
	if _, err := file.Write(data); err != nil {
		file.Close()
		cleanup()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		cleanup()
		return err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return err
	}
	if err := root.Rename(tmp, name); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func removeRooted(dir, name string) error {
	root, err := openRooted(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(filepath.FromSlash(name))
}
