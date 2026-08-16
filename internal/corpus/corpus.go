package corpus

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/clayton/harness-benchmark/internal/doctor"
	"gopkg.in/yaml.v3"
)

//go:embed scenarios/*
var embedded embed.FS

type Repo struct {
	URL              string `yaml:"url" json:"url"`
	BaseRef          string `yaml:"base_ref" json:"base_ref"`
	GoldRef          string `yaml:"gold_ref" json:"gold_ref"`
	EnvironmentPatch string `yaml:"environment_patch" json:"environment_patch,omitempty"`
}

type Acceptance struct {
	SetupCommands []string `yaml:"setup_commands" json:"setup_commands"`
	TestCommands  []string `yaml:"test_commands" json:"test_commands"`
	BuildCommands []string `yaml:"build_commands" json:"build_commands"`
	FailToPass    []string `yaml:"fail_to_pass" json:"fail_to_pass"`
	GoldFiles     []string `yaml:"gold_files" json:"gold_files,omitempty"`
}

type CommandRequirement struct {
	Name           string `yaml:"name" json:"name"`
	MinimumVersion string `yaml:"minimum_version,omitempty" json:"minimum_version,omitempty"`
	Purpose        string `yaml:"purpose" json:"purpose"`
}

type Requirements struct {
	Commands []CommandRequirement `yaml:"commands,omitempty" json:"commands,omitempty"`
}

type Fetch struct {
	Kind     string `yaml:"kind" json:"kind"`
	Lockfile string `yaml:"lockfile,omitempty" json:"lockfile,omitempty"`
	Reason   string `yaml:"reason" json:"reason"`
}

type Workspace struct {
	Kind  string            `yaml:"kind,omitempty" json:"kind,omitempty"`
	Files map[string]string `yaml:"files,omitempty" json:"files,omitempty"`
}

// ScaffoldPaths returns deterministic, canonical paths that are safe to
// materialize before hbench creates its own Git metadata and HB_* files.
func ScaffoldPaths(files map[string]string) ([]string, error) {
	paths := make([]string, 0, len(files))
	seen := make(map[string]bool, len(files))
	for rel := range files {
		clean := filepath.Clean(rel)
		lower := strings.ToLower(clean)
		if rel != clean || clean == "." || filepath.IsAbs(clean) || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
			lower == ".git" || strings.HasPrefix(lower, ".git"+string(filepath.Separator)) ||
			strings.HasPrefix(strings.ToUpper(filepath.Base(clean)), "HB_") {
			return nil, fmt.Errorf("unsafe scaffold path %q", rel)
		}
		if seen[lower] {
			return nil, fmt.Errorf("duplicate scaffold path %q", rel)
		}
		seen[lower] = true
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	return paths, nil
}

type Scenario struct {
	ID                     string       `yaml:"id" json:"id"`
	Type                   string       `yaml:"type" json:"type"`
	Title                  string       `yaml:"title" json:"title"`
	Description            string       `yaml:"description" json:"description"`
	Prompt                 string       `yaml:"prompt" json:"prompt"`
	Language               string       `yaml:"language" json:"language"`
	Tags                   []string     `yaml:"tags" json:"tags"`
	Difficulty             string       `yaml:"difficulty" json:"difficulty"`
	Repo                   Repo         `yaml:"repo" json:"repo"`
	Workspace              Workspace    `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Acceptance             Acceptance   `yaml:"acceptance" json:"acceptance"`
	Requirements           Requirements `yaml:"requirements,omitempty" json:"requirements,omitempty"`
	Fetches                []Fetch      `yaml:"fetches,omitempty" json:"fetches,omitempty"`
	SourceDir              string       `yaml:"-" json:"source_dir,omitempty"`
	Status                 string       `yaml:"status,omitempty" json:"status,omitempty"`
	Version                int          `yaml:"version,omitempty" json:"version,omitempty"`
	ManifestDigest         string       `yaml:"manifest_digest,omitempty" json:"manifest_digest,omitempty"`
	BuiltinScenarioID      string       `yaml:"-" json:"builtin_scenario_id,omitempty"`
	ProtocolID             string       `yaml:"protocol_id,omitempty" json:"protocol_id,omitempty"`
	NetworkPolicy          string       `yaml:"network_policy,omitempty" json:"network_policy,omitempty"`
	EnvironmentImageDigest string       `yaml:"environment_image_digest,omitempty" json:"environment_image_digest,omitempty"`
	External               bool         `yaml:"-" json:"-"`
}

func (s Scenario) ToDoctor() doctor.Scenario {
	return doctor.Scenario{
		ID:         s.ID,
		Language:   s.Language,
		Difficulty: s.Difficulty,
		Tags:       s.Tags,
	}
}

func EnsureCache(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(embedded, "scenarios")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "example-synthetic-bugfix.yaml" {
			continue
		}
		data, err := embedded.ReadFile("scenarios/" + e.Name())
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, e.Name())
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func Load(dir string) ([]Scenario, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if nested, err := filepath.Glob(filepath.Join(dir, "*", "*.yaml")); err == nil {
		matches = append(matches, nested...)
	}
	var out []Scenario
	for _, path := range matches {
		if strings.Contains(filepath.Base(path), "example-synthetic") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var s Scenario
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if s.ID == "" {
			continue
		}
		s.SourceDir = filepath.Dir(path)
		out = append(out, s)
	}
	return out, nil
}

func Find(dir, id string) (Scenario, error) {
	all, err := Load(dir)
	if err != nil {
		return Scenario{}, err
	}
	for _, s := range all {
		if s.ID == id {
			return s, nil
		}
	}
	return Scenario{}, fmt.Errorf("scenario not found: %s", id)
}

func LoadFile(path string) (Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	var s Scenario
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return Scenario{}, fmt.Errorf("%s: %w", path, err)
	}
	if s.ID == "" {
		return Scenario{}, fmt.Errorf("%s: missing id", path)
	}
	s.SourceDir = filepath.Dir(path)
	s.External = true
	return s, nil
}

func Resolve(officialDir, from, idOrPath string) (Scenario, error) {
	if strings.HasPrefix(idOrPath, "rodeo:") {
		return FetchRodeo(officialDir, strings.TrimPrefix(idOrPath, "rodeo:"), nil)
	}
	if idOrPath != "" && (strings.HasSuffix(idOrPath, ".yaml") || strings.HasSuffix(idOrPath, ".yml") || strings.Contains(idOrPath, string(filepath.Separator))) {
		return LoadFile(idOrPath)
	}
	if from != "" {
		s, err := Find(from, idOrPath)
		s.External = err == nil
		return s, err
	}
	return Find(officialDir, idOrPath)
}

var rodeoID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*@[1-9][0-9]*$`)

func RodeoManifestLocation(cacheDir, identifier string) (source, destination string, cached bool, err error) {
	if !rodeoID.MatchString(identifier) {
		return "", "", false, fmt.Errorf("invalid rodeo scenario %q; expected slug@version", identifier)
	}
	parts := strings.SplitN(identifier, "@", 2)
	destination = filepath.Join(cacheDir, "community", strings.ReplaceAll(identifier, "@", "-v")+".json")
	baseURL := strings.TrimRight(os.Getenv("HB_RODEO_URL"), "/")
	if baseURL == "" {
		baseURL = "https://agentrodeo.dev"
	}
	source = fmt.Sprintf("%s/api/v1/scenarios/%s/versions/%s/manifest", baseURL, parts[0], parts[1])
	if raw, readErr := os.ReadFile(destination); readErr == nil {
		_, decodeErr := decodeRodeoManifest(raw, identifier)
		cached = decodeErr == nil
	}
	return source, destination, cached, nil
}

func FetchRodeo(cacheDir, identifier string, client *http.Client) (Scenario, error) {
	url, cachePath, cached, err := RodeoManifestLocation(cacheDir, identifier)
	if err != nil {
		return Scenario{}, err
	}
	if cached {
		raw, _ := os.ReadFile(cachePath)
		scenario, decodeErr := decodeRodeoManifest(raw, identifier)
		if decodeErr != nil {
			return Scenario{}, decodeErr
		}
		return hydrateBuiltinScenario(cacheDir, identifier, scenario)
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	request, _ := http.NewRequest(http.MethodGet, url, nil)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return Scenario{}, fmt.Errorf("fetch rodeo scenario: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Scenario{}, fmt.Errorf("fetch rodeo scenario: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return Scenario{}, err
	}
	if len(raw) > 1<<20 {
		return Scenario{}, fmt.Errorf("rodeo scenario manifest exceeds 1 MiB")
	}
	scenario, err := decodeRodeoManifest(raw, identifier)
	if err != nil {
		return Scenario{}, err
	}
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(cachePath, append(raw, '\n'), 0o644)
	}
	return hydrateBuiltinScenario(cacheDir, identifier, scenario)
}

func decodeRodeoManifest(raw []byte, identifier string) (Scenario, error) {
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Scenario{}, fmt.Errorf("decode rodeo scenario: %w", err)
	}
	claimed := fmt.Sprint(manifest["manifest_digest"])
	digestible := make(map[string]any, len(manifest))
	for key, value := range manifest {
		if key != "manifest_digest" && key != "status" && key != "builtin_scenario_id" {
			digestible[key] = value
		}
	}
	canonical, err := canonicalJSON(digestible)
	if err != nil {
		return Scenario{}, fmt.Errorf("canonicalize rodeo scenario: %w", err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(canonical))
	if claimed == "" || claimed != actual {
		return Scenario{}, fmt.Errorf("rodeo scenario digest mismatch")
	}
	raw, _ = json.Marshal(manifest)
	var scenario Scenario
	if err := json.Unmarshal(raw, &scenario); err != nil {
		return Scenario{}, err
	}
	if scenario.ID != identifier {
		return Scenario{}, fmt.Errorf("rodeo scenario id mismatch: got %q", scenario.ID)
	}
	if scenario.Status != "community" && scenario.Status != "candidate" && scenario.Status != "official" && scenario.Status != "active" && scenario.Status != "runnable" {
		return Scenario{}, fmt.Errorf("rodeo scenario %s is not runnable (status %q)", identifier, scenario.Status)
	}
	if !strings.HasPrefix(scenario.Repo.URL, "https://github.com/") {
		return Scenario{}, fmt.Errorf("rodeo scenario repository must use https://github.com/")
	}
	hex40 := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if !hex40.MatchString(scenario.Repo.BaseRef) || (scenario.Repo.GoldRef != "" && !hex40.MatchString(scenario.Repo.GoldRef)) {
		return Scenario{}, fmt.Errorf("rodeo scenario refs must be full lowercase commit hashes")
	}
	if scenario.Workspace.Kind != "" && scenario.Workspace.Kind != "scaffold" {
		return Scenario{}, fmt.Errorf("unsupported workspace kind %q", scenario.Workspace.Kind)
	}
	scenario.External = true
	return scenario, nil
}

func hydrateBuiltinScenario(cacheDir, identifier string, remote Scenario) (Scenario, error) {
	if remote.BuiltinScenarioID == "" {
		return remote, nil
	}
	slug := strings.SplitN(identifier, "@", 2)[0]
	if remote.BuiltinScenarioID != slug {
		return Scenario{}, fmt.Errorf("rodeo built-in scenario id mismatch")
	}
	local, err := Find(cacheDir, remote.BuiltinScenarioID)
	if err != nil {
		return Scenario{}, fmt.Errorf("this hbench release does not contain built-in scenario %q", remote.BuiltinScenarioID)
	}
	if local.Prompt != remote.Prompt || local.Repo.URL != remote.Repo.URL || local.Repo.BaseRef != remote.Repo.BaseRef ||
		!reflect.DeepEqual(local.Acceptance.SetupCommands, remote.Acceptance.SetupCommands) ||
		!reflect.DeepEqual(local.Acceptance.TestCommands, remote.Acceptance.TestCommands) ||
		!reflect.DeepEqual(local.Requirements, remote.Requirements) || !reflect.DeepEqual(local.Fetches, remote.Fetches) {
		return Scenario{}, fmt.Errorf("built-in scenario %q does not match the published version; update hbench", remote.BuiltinScenarioID)
	}
	local.ID = identifier
	local.Status = remote.Status
	local.Version = remote.Version
	local.ManifestDigest = remote.ManifestDigest
	local.BuiltinScenarioID = remote.BuiltinScenarioID
	local.External = false
	return local, nil
}

// TrustDigest binds approval to the complete executable scenario plus any
// local files it references. The server's manifest_digest is transport
// integrity; this digest is the user's execution approval boundary.
func TrustDigest(s Scenario) (string, error) {
	copy := s
	copy.SourceDir = ""
	copy.External = false
	copy.ManifestDigest = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(raw)
	refs := append([]string{}, s.Acceptance.GoldFiles...)
	if s.Repo.EnvironmentPatch != "" {
		refs = append(refs, s.Repo.EnvironmentPatch)
	}
	for _, rel := range refs {
		path, err := referencedFile(s.SourceDir, rel)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read referenced file %q: %w", rel, err)
		}
		if len(data) > 4<<20 {
			return "", fmt.Errorf("referenced file %q exceeds 4 MiB", rel)
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func referencedFile(sourceDir, rel string) (string, error) {
	if sourceDir == "" || rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe referenced file %q", rel)
	}
	base, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, filepath.Clean(rel))
	within, err := filepath.Rel(base, path)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("referenced file escapes scenario directory: %q", rel)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("referenced file is not a regular file: %q", rel)
	}
	return path, nil
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

// UnmarshalScenarioJSON loads a scenario from a snapshot. New snapshots use the
// same snake_case keys as the YAML. Older snapshots used Go field names
// (Acceptance, TestCommands); those still load.
func UnmarshalScenarioJSON(raw []byte) (Scenario, error) {
	norm, err := rewriteLegacyScenarioJSON(raw)
	if err != nil {
		return Scenario{}, err
	}
	var s Scenario
	if err := json.Unmarshal(norm, &s); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

var scenarioJSONAlias = map[string]string{
	"ID": "id", "Type": "type", "Title": "title", "Description": "description",
	"Prompt": "prompt", "Language": "language", "Tags": "tags",
	"Difficulty": "difficulty", "Repo": "repo", "Acceptance": "acceptance",
	"SourceDir": "source_dir", "URL": "url", "BaseRef": "base_ref",
	"GoldRef": "gold_ref", "EnvironmentPatch": "environment_patch",
	"SetupCommands": "setup_commands", "TestCommands": "test_commands",
	"BuildCommands": "build_commands", "FailToPass": "fail_to_pass",
	"GoldFiles": "gold_files",
}

func rewriteLegacyScenarioJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(rewriteScenarioKeys(v))
}

func rewriteScenarioKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if nk, ok := scenarioJSONAlias[k]; ok {
				k = nk
			}
			out[k] = rewriteScenarioKeys(val)
		}
		return out
	case []any:
		for i, item := range t {
			t[i] = rewriteScenarioKeys(item)
		}
		return t
	default:
		return v
	}
}

func DoctorScenarios(dir string) []doctor.Scenario {
	all, err := Load(dir)
	if err != nil {
		return nil
	}
	out := make([]doctor.Scenario, 0, len(all))
	for _, s := range all {
		out = append(out, s.ToDoctor())
	}
	return out
}
