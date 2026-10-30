package fsio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// The Check* functions refuse where their writing twins refuse, and leave
// the directory exactly as they found it.
func TestChecksRefuseLikeTheWritesAndWriteNothing(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	existing := filepath.Join(root, "acme.md")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A new file where one exists, and replacing a directory.
	if err := CheckPublish(root, existing, true); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("exclusive publish over a file: %v", err)
	}
	if err := WriteNewIn(root, existing, []byte("x")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("the write disagrees: %v", err)
	}
	if err := CheckPublish(root, filepath.Join(root, "dir"), false); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("replacing a directory: %v", err)
	}
	// Fine: a replacement, and a new file under a directory MkdirAll would make.
	if err := CheckPublish(root, existing, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckPublish(root, filepath.Join(root, "new", "deep", "x.md"), true); err != nil {
		t.Fatal(err)
	}
	if err := CheckMkdirAll(root, filepath.Join(root, "new", "deep")); err != nil {
		t.Fatal(err)
	}
	// A file where a directory must go.
	if err := CheckMkdirAll(root, filepath.Join(existing, "sub")); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("mkdir through a file: %v", err)
	}
	if err := CheckRemove(root, filepath.Join(root, "ghost.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("removing nothing: %v", err)
	}
	if err := CheckRemove(root, existing); err != nil {
		t.Fatal(err)
	}
	// Escapes refuse, as OpenPath refuses them for every write.
	if err := CheckPublish(root, filepath.Join(outside, "x"), false); err == nil {
		t.Fatal("escape accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
	} else {
		for _, check := range []func() error{
			func() error { return CheckPublish(root, filepath.Join(root, "link", "x"), true) },
			func() error { return CheckMkdirAll(root, filepath.Join(root, "link", "sub")) },
			func() error { return CheckRemove(root, filepath.Join(root, "link")) },
		} {
			if err := check(); err == nil {
				t.Fatal("a symlink accepted")
			}
		}
		if err := os.Remove(filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || readAll(t, existing) != "keep" {
		t.Fatalf("a check wrote: %v", entries)
	}
}
