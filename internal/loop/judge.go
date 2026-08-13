package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/clayton/harness-benchmark/internal/corpus"
	"github.com/clayton/harness-benchmark/internal/paths"
)

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
