package paths

import (
	"os"
	"path/filepath"
)

const appName = "hb"

// Layout holds on-disk locations. Tests inject dirs instead of using HOME.
type Layout struct {
	Home    string
	DataDir string
	OutDir  string
	Start   string // directory used to discover OutDir (usually cwd)
}

func Default() Layout {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return New(home, cwd)
}

func New(home, cwd string) Layout {
	start := cwd
	if start == "" {
		start, _ = os.Getwd()
	}
	return Layout{
		Home:    home,
		DataDir: dataDir(home),
		OutDir:  FindOutDir(start),
		Start:   start,
	}
}

// FindOutDir walks from start toward the filesystem root and returns the first
// existing hb-out store. If none exists, it returns start/hb-out (the place a
// new run would create).
func FindOutDir(start string) string {
	if override := os.Getenv("HB_OUT_DIR"); override != "" {
		return override
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		abs = start
	}
	dir := abs
	for {
		candidate := filepath.Join(dir, "hb-out")
		if IsStore(candidate) {
			return candidate
		}
		if filepath.Base(dir) == "hb-out" && IsStore(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(abs, "hb-out")
}

// IsStore reports whether dir looks like an hbench artifact tree.
func IsStore(dir string) bool {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "latest")); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "run.json")); err == nil {
			return true
		}
	}
	return false
}

func dataDir(home string) string {
	if override := os.Getenv("HB_DATA_DIR"); override != "" {
		return override
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, appName)
	}
	return filepath.Join(home, ".local", "share", appName)
}

func (l Layout) ScenariosDir() string { return filepath.Join(l.DataDir, "scenarios") }
func (l Layout) ReposDir() string     { return filepath.Join(l.DataDir, "repos") }
func (l Layout) RunDir(id string) string {
	return filepath.Join(l.OutDir, id)
}
func (l Layout) Worktree(id string) string {
	return filepath.Join(l.OutDir, id, "workspace")
}
func (l Layout) ReportFile() string {
	return filepath.Join(l.OutDir, "report.html")
}
func (l Layout) LatestFile() string {
	return filepath.Join(l.OutDir, "latest")
}
