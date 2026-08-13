package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Snapshot is a frozen view of the environment so tests inject PATH/tools.
type Snapshot struct {
	PathBins   map[string]bool
	Toolchains map[string]bool
	SkillNames []string
	Scenarios  []Scenario
}

type Scenario struct {
	ID         string
	Language   string
	Difficulty string
	Tags       []string
}

type Harness struct {
	Name     string
	Headless bool
}

type Suggestion struct {
	Harnesses []Harness
	Manual    []Harness
	Tools     []string
	Skills    []string
	Scenario  string
	Command   string
	Kind      string // execute | prepare | none
	Notes     []string
}

var headlessBins = []struct {
	name string
	bins []string
}{
	{"grok", []string{"grok"}},
	{"pi", []string{"pi"}},
	{"claude", []string{"claude"}},
	{"codex", []string{"codex"}},
}

var manualBins = []struct {
	name string
	bins []string
}{
	{"cursor", []string{"cursor", "cursor-agent"}},
	{"windsurf", []string{"windsurf"}},
	{"aider", []string{"aider"}},
}

var toolchainBins = map[string][]string{
	"node":   {"node"},
	"go":     {"go"},
	"python": {"python3", "python"},
	"ruby":   {"ruby"},
}

var langToTool = map[string]string{
	"javascript": "node",
	"typescript": "node",
	"js":         "node",
	"ts":         "node",
	"go":         "go",
	"python":     "python",
	"ruby":       "ruby",
}

func Probe(lookPath func(string) string) Snapshot {
	s := Snapshot{
		PathBins:   map[string]bool{},
		Toolchains: map[string]bool{},
	}
	check := func(bin string) {
		if lookPath(bin) != "" {
			s.PathBins[bin] = true
		}
	}
	for _, h := range headlessBins {
		for _, b := range h.bins {
			check(b)
		}
	}
	for _, h := range manualBins {
		for _, b := range h.bins {
			check(b)
		}
	}
	for tool, bins := range toolchainBins {
		for _, b := range bins {
			if lookPath(b) != "" {
				s.PathBins[b] = true
				s.Toolchains[tool] = true
			}
		}
	}
	return s
}

func LookPATH(bin string) string {
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	return p
}

func ListSkills(roots []string) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func DefaultSkillRoots(home, cwd string) []string {
	return []string{
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".grok", "skills"),
		filepath.Join(cwd, ".claude", "skills"),
		filepath.Join(cwd, ".agents", "skills"),
	}
}

func Suggest(s Snapshot) Suggestion {
	out := Suggestion{Skills: append([]string(nil), s.SkillNames...)}
	for _, h := range headlessBins {
		if hasAny(s.PathBins, h.bins) {
			out.Harnesses = append(out.Harnesses, Harness{Name: h.name, Headless: true})
		}
	}
	for _, h := range manualBins {
		if hasAny(s.PathBins, h.bins) {
			out.Manual = append(out.Manual, Harness{Name: h.name, Headless: false})
		}
	}
	for _, tool := range []string{"node", "go", "python", "ruby"} {
		if s.Toolchains[tool] {
			out.Tools = append(out.Tools, tool)
		}
	}

	sc := pickScenario(s.Scenarios, s.Toolchains)
	if sc == nil {
		out.Kind = "none"
		out.Notes = append(out.Notes, "no official scenario this machine can judge")
		return out
	}
	out.Scenario = sc.ID

	if len(out.Harnesses) > 0 {
		h := out.Harnesses[0].Name
		out.Kind = "execute"
		out.Command = fmt.Sprintf("hb run -s %s --harness %s && hb execute", sc.ID, h)
		return out
	}
	out.Kind = "prepare"
	out.Command = fmt.Sprintf("hb run -s %s --harness manual", sc.ID)
	out.Notes = append(out.Notes, "no headless harness on PATH; this only prepares a workspace")
	return out
}

func pickScenario(scenarios []Scenario, tools map[string]bool) *Scenario {
	var easy, rest []*Scenario
	for i := range scenarios {
		sc := &scenarios[i]
		need := langToTool[strings.ToLower(sc.Language)]
		if need == "" || !tools[need] {
			continue
		}
		if strings.EqualFold(sc.Difficulty, "easy") || hasTag(sc.Tags, "smoke-ok") {
			easy = append(easy, sc)
		} else {
			rest = append(rest, sc)
		}
	}
	if len(easy) > 0 {
		return easy[0]
	}
	if len(rest) > 0 {
		return rest[0]
	}
	return nil
}

func hasAny(bins map[string]bool, names []string) bool {
	for _, n := range names {
		if bins[n] {
			return true
		}
	}
	return false
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func Format(s Suggestion) string {
	var b strings.Builder
	b.WriteString("hb doctor\n")
	if len(s.Harnesses) > 0 {
		b.WriteString("\nHarnesses (headless):\n")
		for _, h := range s.Harnesses {
			fmt.Fprintf(&b, "  %s\n", h.Name)
		}
	}
	if len(s.Manual) > 0 {
		b.WriteString("\nHarnesses (manual only):\n")
		for _, h := range s.Manual {
			fmt.Fprintf(&b, "  %s\n", h.Name)
		}
	}
	if len(s.Harnesses) == 0 && s.Kind == "prepare" {
		b.WriteString("\nNo headless harness on PATH. The command below only prepares a workspace.\n")
	}
	b.WriteString("\nJudge toolchains:\n")
	if len(s.Tools) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, t := range s.Tools {
		fmt.Fprintf(&b, "  %s\n", t)
	}
	b.WriteString("\nSkills on disk (not attached to the first ride):\n")
	if len(s.Skills) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, n := range s.Skills {
		fmt.Fprintf(&b, "  %s\n", n)
	}
	b.WriteString("\nSuggested next step (paste to run; nothing has been executed):\n")
	if s.Command == "" {
		b.WriteString("  (none: install a judge toolchain or a headless harness)\n")
	} else {
		fmt.Fprintf(&b, "  %s\n", s.Command)
	}
	for _, n := range s.Notes {
		fmt.Fprintf(&b, "\nnote: %s\n", n)
	}
	return b.String()
}
