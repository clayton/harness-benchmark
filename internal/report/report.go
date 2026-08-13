package report

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/clayton/harness-benchmark/internal/loop"
	"github.com/clayton/harness-benchmark/internal/paths"
)

func Write(l paths.Layout) (string, int, error) {
	entries, err := os.ReadDir(l.OutDir)
	if err != nil {
		return "", 0, err
	}
	var rows []string
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(l.OutDir, e.Name(), "run.json"))
		if err != nil {
			continue
		}
		var r loop.RunRecord
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		n++
		q := loop.Quality(r)
		rows = append(rows, fmt.Sprintf(
			"<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%.2f</td></tr>",
			html.EscapeString(r.ID),
			html.EscapeString(r.ScenarioID),
			html.EscapeString(r.Harness),
			html.EscapeString(r.Status),
			q,
		))
	}
	body := strings.Join(rows, "\n")
	page := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><title>hb report</title>
<h1>Harness Benchmark</h1>
<p>%d run(s) in hb-out. Nothing was uploaded.</p>
<table border="1" cellpadding="6"><tr><th>id</th><th>scenario</th><th>harness</th><th>status</th><th>quality</th></tr>
%s
</table>
`, n, body)
	if err := os.MkdirAll(l.OutDir, 0o755); err != nil {
		return "", n, err
	}
	path := l.ReportFile()
	return path, n, os.WriteFile(path, []byte(page), 0o644)
}
