package fetchconsent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Item struct {
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	Ref         string `json:"ref,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
	Reason      string `json:"reason"`
	Destination string `json:"destination"`
	Size        string `json:"size"`
}

type Plan struct {
	Schema string `json:"schema"`
	Items  []Item `json:"items"`
}

func New(items ...Item) Plan {
	copyItems := append([]Item(nil), items...)
	sort.Slice(copyItems, func(i, j int) bool {
		left := copyItems[i].Kind + "\x00" + copyItems[i].Source + "\x00" + copyItems[i].Ref
		right := copyItems[j].Kind + "\x00" + copyItems[j].Source + "\x00" + copyItems[j].Ref
		return left < right
	})
	return Plan{Schema: "hb.fetch-plan.v1", Items: copyItems}
}

func (p Plan) Digest() string {
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type approval struct {
	Digest     string `json:"digest"`
	ApprovedAt string `json:"approved_at"`
}

type store struct {
	Schema    string     `json:"schema"`
	Approvals []approval `json:"approvals"`
}

func StorePath(dataDir string) string { return filepath.Join(dataDir, "fetch-consent.json") }
func PlanPath(dataDir, digest string) string {
	return filepath.Join(dataDir, "fetch-plans", digest+".json")
}

func ValidateDigest(digest string) error {
	if len(digest) != 64 {
		return fmt.Errorf("fetch plan digest must be 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return fmt.Errorf("invalid fetch plan digest")
	}
	return nil
}

func SavePlan(dataDir string, plan Plan) error {
	digest := plan.Digest()
	dir := filepath.Dir(PlanPath(dataDir, digest))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(PlanPath(dataDir, digest), append(raw, '\n'), 0o600)
}

func LoadPlan(dataDir, digest string) (Plan, error) {
	if err := ValidateDigest(digest); err != nil {
		return Plan{}, err
	}
	raw, err := os.ReadFile(PlanPath(dataDir, digest))
	if err != nil {
		return Plan{}, fmt.Errorf("fetch plan not found: %s", digest)
	}
	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Plan{}, err
	}
	if plan.Schema != "hb.fetch-plan.v1" || plan.Digest() != digest {
		return Plan{}, fmt.Errorf("fetch plan digest mismatch")
	}
	return plan, nil
}

func Approved(dataDir, digest string) bool {
	s, err := load(dataDir)
	if err != nil {
		return false
	}
	for _, item := range s.Approvals {
		if item.Digest == digest {
			return true
		}
	}
	return false
}

func Approve(dataDir, digest string) error {
	if _, err := LoadPlan(dataDir, digest); err != nil {
		return err
	}
	s, err := load(dataDir)
	if err != nil {
		return err
	}
	for _, item := range s.Approvals {
		if item.Digest == digest {
			return nil
		}
	}
	s.Approvals = append(s.Approvals, approval{Digest: digest, ApprovedAt: time.Now().UTC().Format(time.RFC3339)})
	return save(dataDir, s)
}

func Revoke(dataDir, digest string) error {
	if err := ValidateDigest(digest); err != nil {
		return err
	}
	s, err := load(dataDir)
	if err != nil {
		return err
	}
	kept := s.Approvals[:0]
	for _, item := range s.Approvals {
		if item.Digest != digest {
			kept = append(kept, item)
		}
	}
	s.Approvals = kept
	return save(dataDir, s)
}

func Format(plan Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fetch plan %s:\n", plan.Digest())
	for _, item := range plan.Items {
		fmt.Fprintf(&b, "  %s\n    source: %s\n", item.Kind, item.Source)
		if item.Ref != "" {
			fmt.Fprintf(&b, "    ref: %s\n", item.Ref)
		}
		if item.Checksum != "" {
			fmt.Fprintf(&b, "    checksum: %s\n", item.Checksum)
		}
		fmt.Fprintf(&b, "    reason: %s\n    destination: %s\n    size: %s\n", item.Reason, item.Destination, item.Size)
	}
	return b.String()
}

func load(dataDir string) (store, error) {
	path := StorePath(dataDir)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store{Schema: "hb.fetch-consent.v1"}, nil
	}
	if err != nil {
		return store{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return store{}, fmt.Errorf("unsafe fetch consent store")
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return store{}, err
		}
	}
	var s store
	if err := json.Unmarshal(raw, &s); err != nil {
		return store{}, err
	}
	if s.Schema != "hb.fetch-consent.v1" {
		return store{}, fmt.Errorf("unsupported fetch consent store")
	}
	return s, nil
}

func save(dataDir string, s store) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(StorePath(dataDir), append(raw, '\n'), 0o600)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hb-fetch-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
