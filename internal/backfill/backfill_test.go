package backfill

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prbe-ai/prbe-agent-tap/internal/storage"
)

func TestEnumerateClampsTo365Days(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-x")
	_ = os.MkdirAll(proj, 0o755)

	old := filepath.Join(proj, "old.jsonl")
	recent := filepath.Join(proj, "new.jsonl")
	_ = os.WriteFile(old, []byte(`{"x":1}`+"\n"), 0o600)
	_ = os.WriteFile(recent, []byte(`{"x":2}`+"\n"), 0o600)

	twoYears := time.Now().Add(-2 * 365 * 24 * time.Hour)
	yesterday := time.Now().Add(-24 * time.Hour)
	_ = os.Chtimes(old, twoYears, twoYears)
	_ = os.Chtimes(recent, yesterday, yesterday)

	files, err := Enumerate(root, time.Now().Add(-2*365*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == old {
			t.Fatalf("365d clamp failed; included %q", old)
		}
	}
}

func TestEnumerateFiltersByMtime(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-y")
	_ = os.MkdirAll(proj, 0o755)

	a := filepath.Join(proj, "a.jsonl")
	b := filepath.Join(proj, "b.jsonl")
	_ = os.WriteFile(a, []byte(`{}`+"\n"), 0o600)
	_ = os.WriteFile(b, []byte(`{}`+"\n"), 0o600)
	_ = os.Chtimes(a, time.Now().Add(-100*24*time.Hour), time.Now().Add(-100*24*time.Hour))
	_ = os.Chtimes(b, time.Now().Add(-1*24*time.Hour), time.Now().Add(-1*24*time.Hour))

	files, err := Enumerate(root, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "b.jsonl" {
		t.Fatalf("got %v want only b.jsonl", files)
	}
}

func TestEnumerateSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-z")
	_ = os.MkdirAll(proj, 0o755)

	target := filepath.Join(proj, "real.jsonl")
	_ = os.WriteFile(target, []byte(`{}`+"\n"), 0o600)
	link := filepath.Join(proj, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	files, _ := Enumerate(root, time.Now().Add(-24*time.Hour))
	for _, f := range files {
		if f == link {
			t.Fatalf("symlink not skipped: %q", link)
		}
	}
}

func TestEnqueueDrainsToTarget(t *testing.T) {
	s, _ := storage.Open(t.TempDir() + "/state.db")
	defer s.Close()

	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	proj := filepath.Join(root, "-Users-q")
	_ = os.MkdirAll(proj, 0o755)
	path := filepath.Join(proj, "s.jsonl")
	_ = os.WriteFile(path, []byte(`{"a":1}`+"\n"+`{"b":2}`+"\n"+`{"c":3}`+"\n"), 0o600)

	cfg := Config{
		Root:          root,
		Since:         time.Now().Add(-time.Hour),
		Storage:       s,
		BatchMaxLines: 2,
		TargetDepth:   1,
		LowWater:      0,
		DeviceIDFunc:  func() (string, error) { return "dev", nil },
		DrainObserver: func() bool { _, _ = s.ClearOutbox(); return true },
	}
	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
}
