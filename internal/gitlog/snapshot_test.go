package gitlog

// The --at snapshot: which commit a value resolves to, what the scratch tree
// holds, and the refusals. The CLI surface is pinned by parity/cases/point-in-time.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func revParse(t *testing.T, root, rev string) string {
	t.Helper()
	out, code, err := run(root, "rev-parse", rev)
	if err != nil || code != 0 {
		t.Fatalf("rev-parse %s: %v (exit %d)", rev, err, code)
	}
	return strings.TrimSpace(out)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// historyRepo is a repository whose khub workspace lives in hub/, with two
// commits a day apart and one uncommitted change.
func historyRepo(t *testing.T) (repo, ws string) {
	t.Helper()
	repo = t.TempDir()
	gitInit(t, repo)
	writeRaw(t, repo, "README.md", "outside the workspace\n")
	ws = filepath.Join(repo, "hub")
	initPreset(t, ws, "firm-ops")
	seed(t, ws, "fragments/first.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	commit(t, repo, "first", "2026-03-01T10:00:00Z")
	seed(t, ws, "fragments/second.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	commit(t, repo, "second", "2026-03-02T23:30:00Z")
	seed(t, ws, "fragments/dirty.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	return repo, ws
}

func TestResolveAt(t *testing.T) {
	repo, ws := historyRepo(t)
	head, first := revParse(t, repo, "HEAD"), revParse(t, repo, "HEAD~1")
	for _, tc := range []struct{ spec, want string }{
		{"HEAD", head},
		{"HEAD~1", first},
		{first[:12], first},
		{"2026-03-01", first},
		// End of day UTC: the 23:30Z commit belongs to its own day.
		{"2026-03-02", head},
		{"2027-01-01", head},
	} {
		got, err := ResolveAt(ws, tc.spec)
		if err != nil {
			t.Fatalf("%s: %v", tc.spec, err)
		}
		if got != tc.want {
			t.Fatalf("%s resolved to %s, want %s", tc.spec, got, tc.want)
		}
	}
	for _, spec := range []string{"2026-02-28", "no-such-branch", "--output=x", "2026-13-45"} {
		_, err := ResolveAt(ws, spec)
		wantCode(t, err, "unknown_revision")
	}
}

func TestMaterializeReadsTheCommitNotTheWorkingTree(t *testing.T) {
	_, ws := historyRepo(t)
	snap, err := Materialize(ws, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	if _, err := os.Stat(filepath.Join(snap.Root, ".khub", "ontology.yaml")); err != nil {
		t.Fatalf("snapshot lacks the schema: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap.Root, "fragments", "first.md")); err != nil {
		t.Fatalf("snapshot lacks first.md: %v", err)
	}
	for _, absent := range []string{"fragments/second.md", "fragments/dirty.md", "README.md", "hub"} {
		if _, err := os.Stat(filepath.Join(snap.Root, filepath.FromSlash(absent))); !os.IsNotExist(err) {
			t.Fatalf("%s should not be in the snapshot (err %v)", absent, err)
		}
	}
	want, _ := os.ReadFile(filepath.Join(ws, "fragments", "first.md"))
	got, _ := os.ReadFile(filepath.Join(snap.Root, "fragments", "first.md"))
	if string(got) != string(want) {
		t.Fatalf("first.md differs:\n%s\nwant\n%s", got, want)
	}
	root := snap.Root
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("Close left %s behind", root)
	}
}

func TestMaterializeRefusals(t *testing.T) {
	// No git at all.
	plain := freshWS(t)
	_, err := Materialize(plain, "HEAD")
	wantCode(t, err, "no_git_history")

	// A commit from before the workspace existed.
	repo := t.TempDir()
	gitInit(t, repo)
	writeRaw(t, repo, "README.md", "x\n")
	commit(t, repo, "before", "2026-01-01T00:00:00Z")
	ws := filepath.Join(repo, "hub")
	initPreset(t, ws, "firm-ops")
	commit(t, repo, "workspace", "2026-02-01T00:00:00Z")
	_, err = Materialize(ws, "HEAD~1")
	wantCode(t, err, "no_workspace")
	snap, err := Materialize(ws, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	_ = snap.Close()
}

func TestStaleFromReadsDatesUpToTheCommit(t *testing.T) {
	repo := freshWS(t)
	gitInit(t, repo)
	seed(t, repo, "fragments/note.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	commit(t, repo, "seed", "2026-01-01T10:00:00Z")
	seed(t, repo, "fragments/note.md", kv{"type", "fragment"}, kv{"stage", "processed"})
	commit(t, repo, "touch", "2026-06-01T10:00:00Z")

	snap, err := Materialize(repo, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	report, err := StaleFrom(snap.Root, snap.History(), ptr(30), now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.GitAvailable || len(report.Entries) != 1 {
		t.Fatalf("want one git-dated entry, got %+v", report)
	}
	if got := report.Entries[0].EffectiveDate.Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("effective date %s, want the snapshot commit's 2026-01-01", got)
	}
	// The same read at HEAD sees the later commit.
	live := mustStale(t, repo, ptr(10))
	if got := live.Entries[0].EffectiveDate.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("live effective date %s, want 2026-06-01", got)
	}
}

func TestMaterializeKeepsModesAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and symlinks are a unix checkout's")
	}
	repo := freshWS(t)
	gitInit(t, repo)
	writeRaw(t, repo, "tools/run.sh", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(repo, "tools", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run.sh", filepath.Join(repo, "tools", "link")); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "seed", "2026-01-01T00:00:00Z")
	snap, err := Materialize(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	info, err := os.Stat(filepath.Join(snap.Root, "tools", "run.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh lost its executable bit: %v %v", info, err)
	}
	target, err := os.Readlink(filepath.Join(snap.Root, "tools", "link"))
	if err != nil || target != "run.sh" {
		t.Fatalf("link: %q %v", target, err)
	}
}

func TestSafeTreePath(t *testing.T) {
	for p, want := range map[string]bool{
		"a/b.md": true, ".khub/ontology.yaml": true, "a..b/c": true,
		"": false, "/etc/passwd": false, "../x": false, "a/../b": false,
		"a//b": false, "./a": false, ".git/config": false, "a/.GIT/x": false, `a\b`: false,
	} {
		if got := safeTreePath(p); got != want {
			t.Fatalf("safeTreePath(%q) = %v, want %v", p, got, want)
		}
	}
}
