package adapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const Schema = "hb.adapter.v1"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var placeholderPattern = regexp.MustCompile(`\$\{[^}]+\}`)
var privatePathPattern = regexp.MustCompile(`(?i)(^|[[:space:]"'=:(])(?:~[/\\]|/Users/|/home/|[A-Z]:\\)`)
var secretValuePattern = regexp.MustCompile(`(?i)(api[_ -]?key|secret|token|password)[[:space:]"']*[:=][[:space:]"']*[A-Za-z0-9_./+@=-]{12,}|bearer[[:space:]]+[A-Za-z0-9._~+/-]{12,}|(sk|ghp|github_pat|xox[baprs])[-_][A-Za-z0-9_-]{12,}|\$\{?[A-Z0-9_]*(TOKEN|SECRET|PASSWORD|API_KEY)\}?`)
var placeholders = map[string]bool{"${prompt}": true, "${prompt_file}": true, "${workspace}": true}

type Manifest struct {
	Schema    string   `yaml:"schema" json:"schema"`
	ID        string   `yaml:"id" json:"id"`
	Version   string   `yaml:"version" json:"version"`
	Command   []string `yaml:"command" json:"command"`
	Telemetry string   `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
}

func FromMap(value map[string]any) (Manifest, string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return Manifest{}, "", err
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("parse adapter: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, "", err
	}
	return manifest, Digest(manifest), nil
}

func Load(path string) (Manifest, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, "", err
	}
	var m Manifest
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(&m); err != nil {
		return Manifest{}, "", fmt.Errorf("parse adapter: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, "", err
	}
	return m, Digest(m), nil
}

func (m Manifest) Validate() error {
	if m.Schema != Schema {
		return fmt.Errorf("schema must be %s", Schema)
	}
	if !idPattern.MatchString(m.ID) || strings.TrimSpace(m.Version) == "" || len(m.Version) > 128 {
		return fmt.Errorf("adapter id and version are required")
	}
	if len(m.Command) == 0 || len(m.Command) > 64 || strings.TrimSpace(m.Command[0]) == "" {
		return fmt.Errorf("adapter command is required and limited to 64 arguments")
	}
	program := m.Command[0]
	if strings.HasPrefix(program, "/") || strings.HasPrefix(program, "~") || regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(program) {
		return fmt.Errorf("adapter program must resolve through PATH")
	}
	shell := strings.ToLower(filepath.Base(program))
	if map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true, "cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true, "pwsh": true}[shell] {
		return fmt.Errorf("adapter command must not invoke a shell; use a dedicated executable with argument-array placeholders")
	}
	for _, arg := range m.Command {
		if len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("adapter arguments are limited to 4096 bytes")
		}
		if privatePathPattern.MatchString(arg) || secretValuePattern.MatchString(arg) {
			return fmt.Errorf("adapter arguments contain private path or secret-like data")
		}
		for _, field := range placeholderPattern.FindAllString(arg, -1) {
			if !placeholders[field] {
				return fmt.Errorf("unsupported adapter placeholder")
			}
		}
		if strings.Contains(placeholderPattern.ReplaceAllString(arg, ""), "${") {
			return fmt.Errorf("unsupported adapter placeholder")
		}
	}
	if m.Telemetry != "" && !map[string]bool{"none": true, "pi": true, "grok": true, "codex": true, "cursor": true}[m.Telemetry] {
		return fmt.Errorf("unsupported telemetry parser %q", m.Telemetry)
	}
	return nil
}

func Digest(m Manifest) string {
	raw, _ := yaml.Marshal(m)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func Launch(m Manifest, prompt, workspace string) (program string, args []string) {
	if len(m.Command) == 0 {
		return "", nil
	}
	program = m.Command[0]
	for _, value := range m.Command[1:] {
		value = strings.ReplaceAll(value, "${prompt_file}", "HB_PROMPT.txt")
		value = strings.ReplaceAll(value, "${workspace}", workspace)
		value = strings.ReplaceAll(value, "${prompt}", prompt)
		args = append(args, value)
	}
	return program, args
}
