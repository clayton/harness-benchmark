package report

import (
	"fmt"
	"html"
	"net/url"
	"os"
	"strings"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
	studycontract "github.com/clayton/harness-benchmark/internal/study"
)

func Write(l paths.Layout) (string, int, error) { return write(l, "", nil, nil, "") }

func WriteStudyRuns(l paths.Layout, studyID string, runIDs map[string]bool) (string, int, error) {
	return write(l, studyID, runIDs, nil, "")
}

// WriteStudyComparison writes the local report for a frozen contract. It
// includes per-arm repeat/cost completeness and a reproducible run command;
// it never uploads runs or the contract.
func WriteStudyComparison(l paths.Layout, m studycontract.Manifest, runIDs map[string]bool, manifestPath string) (string, int, error) {
	return write(l, m.ID, runIDs, &m, manifestPath)
}

func write(l paths.Layout, studyID string, runIDs map[string]bool, manifest *studycontract.Manifest, manifestPath string) (string, int, error) {
	entries, err := os.ReadDir(l.OutDir)
	if err != nil {
		return "", 0, err
	}
	var cards []string
	var studyRuns []loop.RunRecord
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := loop.Load(l, e.Name())
		if err != nil {
			continue
		}
		if studyID != "" {
			if runIDs != nil && !runIDs[r.ID] {
				continue
			}
			profile, _ := r.Metadata["profile"].(map[string]any)
			if profile == nil || profile["study_id"] != studyID {
				continue
			}
		}
		n++
		cards = append(cards, renderRun(r))
		studyRuns = append(studyRuns, r)
	}
	body := strings.Join(cards, "\n")
	summary := ""
	if manifest != nil {
		summary = renderStudySummary(*manifest, studyRuns, manifestPath)
	}
	page := fmt.Sprintf(`<!doctype html>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<title>hbench report</title>
<style>
  body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 2rem auto; max-width: 52rem; color: #111; }
  h1 { font-size: 1.25rem; margin-bottom: 0.25rem; }
  .muted { color: #555; }
  article { border: 1px solid #ddd; border-radius: 8px; padding: 1rem 1.25rem; margin: 1rem 0; }
  article h2 { font-size: 1rem; margin: 0 0 0.5rem; }
  table { border-collapse: collapse; width: 100%%; margin-top: 0.5rem; }
  th, td { text-align: left; padding: 0.25rem 0.4rem; border-bottom: 1px solid #eee; font-size: 0.9rem; }
  .pass { color: #157347; }
  .fail { color: #b42318; }
  a { color: inherit; }
</style>
<h1>hbench report</h1>
<p class="muted">%d run(s) in hb-out%s. Nothing was uploaded.</p>
%s
<p class="muted">Optional: <code>hbench publish</code> uploads a finished run. It is not automatic.</p>
%s
`, n, func() string {
		if studyID == "" {
			return ""
		}
		return " for study " + html.EscapeString(studyID)
	}(), body, summary)
	if err := os.MkdirAll(l.OutDir, 0o755); err != nil {
		return "", n, err
	}
	path := l.ReportFile()
	return path, n, loop.WriteFileAtomic(path, []byte(page), 0o600)
}

func renderStudySummary(m studycontract.Manifest, runs []loop.RunRecord, manifestPath string) string {
	type armStats struct {
		id                    string
		runs, repeats, passed int
		cost                  float64
		costKnown             bool
	}
	stats := make(map[string]*armStats, len(m.Arms))
	for _, arm := range m.Arms {
		stats[arm.ID] = &armStats{id: arm.ID}
	}
	for _, r := range runs {
		armID := ""
		if profile, ok := r.Metadata["profile"].(map[string]any); ok {
			armID, _ = profile["arm_id"].(string)
		}
		if armID == "" {
			armID = r.ConfigID
		}
		s := stats[armID]
		if s == nil {
			s = &armStats{id: armID}
			stats[armID] = s
		}
		s.runs++
		if r.Judges != nil {
			s.repeats++
		}
		for _, judge := range r.Judges {
			if judge.Passed != nil && *judge.Passed {
				s.passed++
				break
			}
		}
		if r.Telemetry.EstimatedUSD == nil || r.Telemetry.Complete == nil || !*r.Telemetry.Complete ||
			(r.Telemetry.CostKind != "actual" && r.Telemetry.CostKind != "estimated") ||
			(r.Telemetry.CostKind == "estimated" && r.Telemetry.PriceSnapshot == "") {
			s.costKnown = false
		} else {
			if s.runs == 1 {
				s.costKnown = true
			}
			s.cost += *r.Telemetry.EstimatedUSD
		}
	}
	var table strings.Builder
	table.WriteString(`<section><h2>Study comparison</h2><table><tr><th>arm</th><th>repeats</th><th>passed</th><th>cost</th><th>cost telemetry</th></tr>`)
	for _, arm := range m.Arms {
		s := stats[arm.ID]
		cost := "incomplete"
		if s.costKnown {
			cost = fmt.Sprintf("$%.6f", s.cost)
		}
		fmt.Fprintf(&table, `<tr><td>%s</td><td>%d/%d</td><td>%d</td><td>%s</td><td>%s</td></tr>`, html.EscapeString(arm.ID), s.repeats, len(m.Scenarios)*m.Repeats, s.passed, html.EscapeString(cost), map[bool]string{true: "complete", false: "incomplete"}[s.costKnown])
	}
	table.WriteString(`</table>`)
	if manifestPath == "" {
		manifestPath = "STUDY.yaml"
	}
	fmt.Fprintf(&table, `<p class="muted">Reproduce locally: <code>hbench study run %s --approve-spend</code></p></section>`, html.EscapeString(manifestPath))
	return table.String()
}

func renderRun(r loop.RunRecord) string {
	q := loop.Quality(r)
	var judges strings.Builder
	if len(r.Judges) > 0 {
		judges.WriteString(`<table><tr><th>judge</th><th></th><th>score</th><th>notes</th></tr>`)
		for _, j := range r.Judges {
			mark, cls := "—", ""
			if j.Passed != nil {
				if *j.Passed {
					mark, cls = "pass", "pass"
				} else {
					mark, cls = "fail", "fail"
				}
			}
			fmt.Fprintf(&judges, `<tr><td>%s</td><td class="%s">%s</td><td>%.2f</td><td>%s</td></tr>`,
				html.EscapeString(j.Name), cls, mark, j.Score, html.EscapeString(j.Notes))
		}
		judges.WriteString(`</table>`)
	}
	when := r.CreatedAt
	if r.FinishedAt != "" {
		when = r.FinishedAt
	}
	idPath := "./" + url.PathEscape(r.ID)
	return fmt.Sprintf(`<article>
<h2>%s · quality %.2f</h2>
<p>%s · %s · %s · %s</p>
<p><a href="%s/patch.diff">patch.diff</a> · <a href="%s/run.json">run.json</a></p>
%s
</article>`,
		html.EscapeString(r.ID), q,
		html.EscapeString(r.ScenarioID), html.EscapeString(r.Harness), html.EscapeString(r.Status), html.EscapeString(when),
		html.EscapeString(idPath), html.EscapeString(idPath),
		judges.String(),
	)
}
