package report

import (
	"fmt"
	"html"
	"net/url"
	"os"
	"strings"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func Write(l paths.Layout) (string, int, error) {
	entries, err := os.ReadDir(l.OutDir)
	if err != nil {
		return "", 0, err
	}
	var cards []string
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := loop.Load(l, e.Name())
		if err != nil {
			continue
		}
		n++
		cards = append(cards, renderRun(r))
	}
	body := strings.Join(cards, "\n")
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
<p class="muted">%d run(s) in hb-out. Nothing was uploaded.</p>
%s
<p class="muted">Optional: <code>hbench publish</code> uploads a finished run. It is not automatic.</p>
`, n, body)
	if err := os.MkdirAll(l.OutDir, 0o755); err != nil {
		return "", n, err
	}
	path := l.ReportFile()
	return path, n, loop.WriteFileAtomic(path, []byte(page), 0o600)
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
