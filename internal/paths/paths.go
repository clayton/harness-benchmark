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
}

func Default() Layout {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return New(home, cwd)
}

func New(home, cwd string) Layout {
	return Layout{
		Home:    home,
		DataDir: dataDir(home),
		OutDir:  filepath.Join(cwd, "hb-out"),
	}
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
