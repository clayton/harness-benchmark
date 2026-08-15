package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed run-agent-rodeo-study/**
var bundled embed.FS

func Install(target string) error {
	if target == "" {
		return fmt.Errorf("target skill directory is required")
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	root := filepath.Join(target, "run-agent-rodeo-study")
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("skill already exists at %s", root)
	}
	return fs.WalkDir(bundled, "run-agent-rodeo-study", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := strings.TrimPrefix(path, "run-agent-rodeo-study")
		dest := filepath.Join(root, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		raw, err := bundled.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0o644)
	})
}
