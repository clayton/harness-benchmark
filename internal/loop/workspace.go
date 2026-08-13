package loop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func repoSlug(url string) string {
	u := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	parts := strings.Split(u, "/")
	name := parts[len(parts)-1]
	owner := "repo"
	if len(parts) >= 2 {
		owner = parts[len(parts)-2]
		if i := strings.LastIndex(owner, ":"); i >= 0 {
			owner = owner[i+1:]
		}
	}
	return owner + "__" + name
}

func ensureRepo(l paths.Layout, sc corpus.Scenario) (string, error) {
	cache := filepath.Join(l.ReposDir(), repoSlug(sc.Repo.URL))
	// Leftover blobless caches cannot serve historical blobs to a worktree.
	if isPartialClone(cache) {
		if err := os.RemoveAll(cache); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(cache, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
			return "", err
		}
		if err := runGit("", "clone", sc.Repo.URL, cache); err != nil {
			return "", fmt.Errorf("clone %s: %w", sc.Repo.URL, err)
		}
	}
	if err := fetchRef(cache, sc.Repo.BaseRef); err != nil {
		return "", fmt.Errorf("fetch base_ref %s: %w", sc.Repo.BaseRef, err)
	}
	if sc.Repo.GoldRef != "" {
		if err := fetchRef(cache, sc.Repo.GoldRef); err != nil {
			return "", fmt.Errorf("fetch gold_ref %s: %w", sc.Repo.GoldRef, err)
		}
	}
	return cache, nil
}

func isPartialClone(repo string) bool {
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return false
	}
	out, err := exec.Command("git", "-C", repo, "config", "--get", "remote.origin.partialclonefilter").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func fetchRef(repo, ref string) error {
	if ref == "" {
		return nil
	}
	if err := git(repo, "cat-file", "-t", ref); err == nil {
		return nil
	}
	if err := git(repo, "fetch", "--depth=1", "origin", ref); err != nil {
		if err2 := git(repo, "fetch", "origin", ref); err2 != nil {
			return err
		}
	}
	return git(repo, "cat-file", "-t", ref)
}

func PrepareWorktree(l paths.Layout, sc corpus.Scenario, runID string, runSetup bool) (string, error) {
	cache, err := ensureRepo(l, sc)
	if err != nil {
		return "", err
	}
	dest := l.Worktree(runID)
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// Clone from the scenario remote, not the cache. A worktree whose origin is
	// a blobless cache cannot fetch historical blobs (chi.go / wrap_writer.go).
	if err := runGit("", "clone", sc.Repo.URL, dest); err != nil {
		return "", fmt.Errorf("worktree clone: %w", err)
	}
	if err := fetchRef(dest, sc.Repo.BaseRef); err != nil {
		_ = git(dest, "remote", "remove", "hb-cache")
		if err := git(dest, "remote", "add", "hb-cache", cache); err == nil {
			_ = git(dest, "fetch", "hb-cache", sc.Repo.BaseRef)
		}
	}
	if err := git(dest, "checkout", "-f", "--detach", sc.Repo.BaseRef); err != nil {
		return "", fmt.Errorf("checkout %s: %w", sc.Repo.BaseRef, err)
	}
	exclude := filepath.Join(dest, ".git", "info", "exclude")
	_ = os.MkdirAll(filepath.Dir(exclude), 0o755)
	_ = os.WriteFile(exclude, []byte("HB_PROMPT.txt\nHB_RUN.md\nHB_LAUNCH.md\n"), 0o644)
	if err := os.WriteFile(filepath.Join(dest, "HB_PROMPT.txt"), []byte(strings.TrimSpace(sc.Prompt)+"\n"), 0o644); err != nil {
		return "", err
	}
	guide := fmt.Sprintf("# %s\n\nFeed the agent only HB_PROMPT.txt.\nThen: hb finish %s\n", sc.Title, runID)
	_ = os.WriteFile(filepath.Join(dest, "HB_RUN.md"), []byte(guide), 0o644)
	if runSetup {
		for _, cmd := range sc.Acceptance.SetupCommands {
			c := exec.Command("sh", "-c", cmd)
			c.Dir = dest
			if out, err := c.CombinedOutput(); err != nil {
				return "", fmt.Errorf("setup %q: %w\n%s", cmd, err, out)
			}
		}
	}
	return dest, nil
}

func gitDiff(worktree string) (string, error) {
	cmd := exec.Command("git", "-C", worktree, "diff", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func git(dir string, args ...string) error {
	return runGit(dir, args...)
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}
