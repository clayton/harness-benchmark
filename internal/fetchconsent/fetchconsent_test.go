package fetchconsent

import (
	"os"
	"testing"
)

func TestApprovalIsBoundToExactPlan(t *testing.T) {
	dir := t.TempDir()
	plan := New(Item{Kind: "Git repository", Source: "https://example.test/repo.git", Ref: "abc123", Reason: "benchmark source", Destination: "/tmp/repo", Size: "unknown"})
	if err := SavePlan(dir, plan); err != nil {
		t.Fatal(err)
	}
	if err := Approve(dir, plan.Digest()); err != nil {
		t.Fatal(err)
	}
	if !Approved(dir, plan.Digest()) {
		t.Fatal("exact plan was not approved")
	}
	changed := New(Item{Kind: "Git repository", Source: "https://example.test/repo.git", Ref: "different", Reason: "benchmark source", Destination: "/tmp/repo", Size: "unknown"})
	if Approved(dir, changed.Digest()) {
		t.Fatal("changed plan inherited approval")
	}
	if err := Revoke(dir, plan.Digest()); err != nil {
		t.Fatal(err)
	}
	if Approved(dir, plan.Digest()) {
		t.Fatal("revoked plan is still approved")
	}
}

func TestConsentFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	plan := New(Item{Kind: "input", Source: "https://example.test/input", Reason: "test", Destination: "/tmp/input", Size: "1 byte"})
	if err := SavePlan(dir, plan); err != nil {
		t.Fatal(err)
	}
	if err := Approve(dir, plan.Digest()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{PlanPath(dir, plan.Digest()), StorePath(dir)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode=%o", path, got)
		}
	}
}
