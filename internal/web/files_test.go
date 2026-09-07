package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The guards matter more than the happy path: this is the one destructive
// operation in the program, and both inputs originate with a user.
func TestRemoveDataRefusesDangerousInput(t *testing.T) {
	base := t.TempDir()
	cases := []struct{ dir, name, why string }{
		{base, "", "no name yet — deleting dir would take the whole download directory"},
		{"", "x", "no directory"},
		{base, "..", "climbs out"},
		{base, "../evil", "climbs out"},
		{base, "sub/evil", "not a single path element"},
		{"/", "etc", "refuses the filesystem root"},
	}
	for _, c := range cases {
		if err := removeData(c.dir, c.name); err == nil {
			t.Errorf("removeData(%q,%q) was allowed; %s", c.dir, c.name, c.why)
		}
	}
}

func TestRemoveDataDeletesTheTorrentPayload(t *testing.T) {
	base := t.TempDir()
	// multi-file torrent: a directory
	multi := filepath.Join(base, "Some.Release")
	if err := os.MkdirAll(filepath.Join(multi, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(multi, "inner", "a.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a sibling that must survive
	keep := filepath.Join(base, "unrelated.bin")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removeData(base, "Some.Release"); err != nil {
		t.Fatalf("removeData: %v", err)
	}
	if _, err := os.Stat(multi); !os.IsNotExist(err) {
		t.Error("payload directory survived")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("an unrelated sibling was deleted")
	}
}

// Deleting something already gone is success: the caller's goal is "the entry
// and its bytes are gone", and a missing file already satisfies half of it.
func TestRemoveDataIsIdempotent(t *testing.T) {
	base := t.TempDir()
	if err := removeData(base, "never-existed"); err != nil {
		t.Fatalf("expected success for an absent payload, got %v", err)
	}
}

func TestFullPathIsAbsolute(t *testing.T) {
	got := fullPath(".", "thing")
	if !strings.HasPrefix(got, "/") || !strings.HasSuffix(got, "/thing") {
		t.Fatalf("fullPath = %q, want an absolute path ending in /thing", got)
	}
	if d := fullPath(".", ""); !strings.HasSuffix(d, string(os.PathSeparator)) {
		t.Errorf("an unresolved torrent should show its directory, got %q", d)
	}
}
