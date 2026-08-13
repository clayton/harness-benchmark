package loop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

var testPathRe = regexp.MustCompile(`(?i)(^|/)(test|tests|spec|__tests__)(/|$)|(test|spec)\.[a-z0-9]+$`)

func Finish(l paths.Layout, id string, sc corpus.Scenario, wallMS int, notes string) (RunRecord, error) {
	rec, err := Load(l, id)
	if err != nil {
		return RunRecord{}, err
	}
	diff, err := gitDiff(rec.Worktree)
	if err != nil {
		diff = ""
	}
	_ = os.WriteFile(filepath.Join(l.RunDir(id), "patch.diff"), []byte(diff), 0o644)
	trueVal := true
	falseVal := false
	var judges []JudgeScore
	if strings.TrimSpace(diff) == "" {
		judges = append(judges, JudgeScore{Name: "non_empty_patch", Score: 0, Passed: &falseVal, Notes: "empty patch"})
	} else {
		judges = append(judges, JudgeScore{Name: "non_empty_patch", Score: 1, Passed: &trueVal, Notes: "patch has content"})
	}

	var restore func()
	if sc.Repo.GoldRef != "" {
		cache, cerr := ensureRepo(l, sc)
		if cerr != nil {
			return rec, fmt.Errorf("gold cache: %w", cerr)
		}
		applied, backups, oerr := overlayGoldTests(cache, rec.Worktree, sc)
		if oerr != nil {
			restoreOverlays(rec.Worktree, backups)
			return rec, fmt.Errorf("gold overlay: %w", oerr)
		}
		restore = func() { restoreOverlays(rec.Worktree, backups) }
		_ = os.WriteFile(filepath.Join(l.RunDir(id), "judge-gold-overlay.txt"), []byte(strings.Join(applied, "\n")+"\n"), 0o644)
		note := fmt.Sprintf("Applied %d gold test file(s): %s", len(applied), strings.Join(applied, ", "))
		if len(sc.Acceptance.FailToPass) > 0 {
			note += "; fail_to_pass=" + strings.Join(sc.Acceptance.FailToPass, ", ")
		}
		if len(applied) > 0 {
			judges = append(judges, JudgeScore{Name: "gold_test_overlay", Score: 1, Passed: &trueVal, Notes: note})
		} else {
			judges = append(judges, JudgeScore{Name: "gold_test_overlay", Notes: note})
		}
	}
	if restore != nil {
		defer restore()
	}

	ok := true
	var notesParts []string
	for _, cmd := range sc.Acceptance.TestCommands {
		c := exec.Command("sh", "-c", cmd)
		c.Dir = rec.Worktree
		out, err := c.CombinedOutput()
		if err != nil {
			ok = false
			notesParts = append(notesParts, cmd+" failed")
		} else {
			notesParts = append(notesParts, cmd+" ok")
		}
		_ = os.WriteFile(filepath.Join(l.RunDir(id), "judge-"+sanitize(cmd)+".txt"), out, 0o644)
	}
	if len(sc.Acceptance.TestCommands) == 0 {
		judges = append(judges, JudgeScore{Name: "acceptance_tests", Notes: "no test_commands"})
	} else if ok {
		judges = append(judges, JudgeScore{Name: "acceptance_tests", Score: 1, Passed: &trueVal, Notes: strings.Join(notesParts, "; ")})
	} else {
		judges = append(judges, JudgeScore{Name: "acceptance_tests", Score: 0, Passed: &falseVal, Notes: strings.Join(notesParts, "; ")})
	}
	rec.Judges = judges
	rec.Notes = notes
	rec.Telemetry.WallMS = wallMS
	rec.FinishedAt = Now()
	if ok && strings.TrimSpace(diff) != "" {
		rec.Status = "completed"
	} else {
		rec.Status = "failed"
	}
	if err := Save(l, rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func isTestPath(p string) bool {
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".rst") || strings.HasSuffix(lower, ".txt") {
		return false
	}
	return testPathRe.MatchString(p)
}

func listGoldTestFiles(cache string, sc corpus.Scenario) []string {
	if sc.Repo.GoldRef == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", cache, "diff", "--name-only", sc.Repo.BaseRef, sc.Repo.GoldRef)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		p := strings.TrimSpace(line)
		if p != "" && isTestPath(p) {
			paths = append(paths, p)
		}
	}
	return paths
}

func overlayGoldTests(cache, worktree string, sc corpus.Scenario) ([]string, map[string]*string, error) {
	backups := map[string]*string{}
	var applied []string
	for _, rel := range listGoldTestFiles(cache, sc) {
		target := filepath.Join(worktree, filepath.FromSlash(rel))
		if raw, err := os.ReadFile(target); err == nil {
			s := string(raw)
			backups[rel] = &s
		} else {
			backups[rel] = nil
		}
		show := exec.Command("git", "-C", cache, "show", sc.Repo.GoldRef+":"+rel)
		content, err := show.Output()
		if err != nil {
			return applied, backups, fmt.Errorf("show %s:%s: %w", sc.Repo.GoldRef, rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return applied, backups, err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return applied, backups, err
		}
		applied = append(applied, rel)
	}
	return applied, backups, nil
}

func restoreOverlays(worktree string, backups map[string]*string) {
	for rel, prev := range backups {
		target := filepath.Join(worktree, filepath.FromSlash(rel))
		if prev == nil {
			_ = os.Remove(target)
			continue
		}
		_ = os.WriteFile(target, []byte(*prev), 0o644)
	}
}

func Quality(r RunRecord) float64 {
	var n int
	var s float64
	for _, j := range r.Judges {
		if j.Passed != nil {
			s += j.Score
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return s / float64(n)
}

func sanitize(cmd string) string {
	out := make([]rune, 0, len(cmd))
	for _, r := range cmd {
		if r == '/' || r == ' ' || r == '.' {
			out = append(out, '-')
		} else {
			out = append(out, r)
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return string(out)
}
