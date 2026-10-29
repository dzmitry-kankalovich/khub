// Package doctor is `khub doctor`: a read-only self-diagnosis of what every
// other command assumes before it starts — a workspace, git, a schema that
// resolves, a readable provenance stamp, a lock a write can take, and storage
// shaped the way the schema lays it out. `validate` and `check` judge the
// entities of a working workspace; doctor judges whether the workspace works.
//
// Doctor writes nothing. The lock probe opens an existing lock file for
// writing without taking it, or creates and removes one probe file in the
// nearest directory above where the lock would go, so no file is left behind
// and a writer holding the lock never blocks it.
package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// The statuses a check reports. Only Fail fails the run.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
	Skip = "skip"
)

// Check is one diagnosis. When Err is set, the CLI renders it the way every
// other command renders that error and uses its code; Code and Message are
// the fallback for an error the CLI does not recognise, and the whole
// finding otherwise.
type Check struct {
	Name    string
	Status  string
	Code    string
	Message string
	Err     error
}

// Report is every check in its fixed order. Root is "" when no workspace
// was found.
type Report struct {
	Root   string
	Checks []Check
}

// Passed reports whether no check failed. A warning does not fail.
func (r *Report) Passed() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return false
		}
	}
	return true
}

// Run diagnoses the workspace at root. rootErr is what resolving the
// workspace returned; when it is set, every later check is skipped.
func Run(root string, rootErr error) *Report {
	if rootErr != nil {
		r := &Report{Checks: []Check{{Name: "workspace", Status: Fail, Code: "no_workspace", Err: rootErr}}}
		for _, name := range []string{"git", "schema", "provenance", "lock", "layout"} {
			r.Checks = append(r.Checks, Check{Name: name, Status: Skip, Message: "Skipped: no workspace"})
		}
		return r
	}
	r := &Report{Root: root}
	r.Checks = append(r.Checks, Check{Name: "workspace", Status: OK, Message: filepath.ToSlash(root)})
	r.Checks = append(r.Checks, checkGit(root))
	resolved, layers, err := introspect.LoadSchemaLayers(root)
	if err != nil {
		r.Checks = append(r.Checks, Check{Name: "schema", Status: Fail, Code: "schema_error", Err: err})
	} else {
		r.Checks = append(r.Checks, Check{Name: "schema", Status: OK, Message: fmt.Sprintf(
			"%s resolved from %s", plural(resolved.Types.Len(), "type"), strings.Join(layers, ", "))})
	}
	r.Checks = append(r.Checks, checkProvenance(root))
	r.Checks = append(r.Checks, checkLock(root))
	if resolved == nil {
		r.Checks = append(r.Checks, Check{Name: "layout", Status: Skip, Message: "Skipped: the schema does not resolve"})
	} else {
		r.Checks = append(r.Checks, checkLayout(root, resolved)...)
	}
	return r
}

// checkGit asks git for the workspace's history the way stale and backfill
// do. No git at all stops both; no commit yet leaves them without dates.
func checkGit(root string) Check {
	ok, err := gitlog.HasGitHistory(root)
	if err != nil {
		msg := "git is not on the PATH; stale and backfill cannot run without it. Install git"
		if !errors.Is(err, exec.ErrNotFound) {
			msg = "Cannot run git (" + err.Error() + "); stale and backfill cannot run without it"
		}
		return Check{Name: "git", Status: Fail, Code: "git_unavailable", Message: msg}
	}
	if !ok {
		return Check{Name: "git", Status: Warn, Code: "no_git_history", Message: "The workspace has no git commit; " +
			"stale and backfill read no dates from git until it is committed"}
	}
	return Check{Name: "git", Status: OK, Message: "git found; the workspace has commit history"}
}

// checkProvenance reads .khub/config.yaml the way every command that loads it
// does: the preset stamp, then the stale threshold.
func checkProvenance(root string) Check {
	const config = ".khub/config.yaml"
	prov, err := workspace.Provenance(root)
	if err != nil {
		return Check{Name: "provenance", Status: Fail, Code: "provenance_unreadable", Message: config, Err: err}
	}
	if _, err := workspace.StaleDays(root); err != nil {
		return Check{Name: "provenance", Status: Fail, Code: "provenance_unreadable", Message: config, Err: err}
	}
	preset, _ := prov.Get("preset")
	version, _ := prov.Get("version")
	var missing []string
	if preset == "" {
		missing = append(missing, "preset")
	}
	if version == "" {
		missing = append(missing, "version")
	}
	if len(missing) > 0 {
		return Check{Name: "provenance", Status: Warn, Code: "provenance_incomplete", Message: fmt.Sprintf(
			"%s records no %s; upgrade cannot tell what to refresh from", config, strings.Join(missing, " or "))}
	}
	return Check{Name: "provenance", Status: OK, Message: fmt.Sprintf("preset %s, version %s", preset, version)}
}

// checkLock proves a write could take the workspace lock without taking it.
func checkLock(root string) Check {
	lockPath := fsio.WorkspaceLockPath(root)
	shown := relSlash(root, lockPath)
	fail := func(err error) Check {
		return Check{Name: "lock", Status: Fail, Code: "lock_unwritable", Message: shown, Err: err}
	}
	r, rel, err := fsio.OpenPath(root, lockPath)
	if err != nil {
		return fail(err)
	}
	defer r.Close()
	info, err := r.Lstat(rel)
	if err == nil {
		if !info.Mode().IsRegular() {
			return Check{Name: "lock", Status: Fail, Code: "lock_unwritable", Message: fmt.Sprintf(
				"%s is not a regular file; every write opens it as the workspace lock", shown)}
		}
		f, err := r.OpenFile(rel, os.O_RDWR, 0)
		if err != nil {
			return fail(fsio.FileInPath(err, root, lockPath))
		}
		_ = f.Close()
		return Check{Name: "lock", Status: OK, Message: shown + " opens for writing"}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fail(fsio.FileInPath(err, root, lockPath))
	}
	// The lock does not exist yet: the first write creates it and the
	// directories above it, so the nearest existing directory must take a
	// new file.
	dir := filepath.Dir(rel)
	for {
		info, err := r.Lstat(dir)
		if err == nil {
			if !info.IsDir() {
				return Check{Name: "lock", Status: Fail, Code: "lock_unwritable", Message: fmt.Sprintf(
					"%s is not a directory; the workspace lock lives under it", relSlash(root, filepath.Join(root, dir)))}
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fail(fsio.FileInPath(err, root, filepath.Join(root, dir)))
		}
		dir = filepath.Dir(dir)
	}
	if err := probeDir(r, root, dir); err != nil {
		return fail(err)
	}
	return Check{Name: "lock", Status: OK, Message: shown + " can be created"}
}

// probeDir creates one exclusive probe file in dir and removes it. An error
// names dir, never the probe, so the message does not carry a process id.
func probeDir(r *os.Root, root, dir string) error {
	for n := 0; ; n++ {
		name := filepath.Join(dir, fmt.Sprintf(".doctor-probe-%d-%d", os.Getpid(), n))
		f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) && n < 8 {
			continue
		}
		if err != nil {
			var pe *fs.PathError
			if errors.As(err, &pe) {
				return &fs.PathError{Op: pe.Op, Path: filepath.Join(root, dir), Err: pe.Err}
			}
			return err
		}
		_ = f.Close()
		return r.Remove(name)
	}
}

// checkLayout holds every declared type's storage against its layout: each
// storage root that exists is the shape the layout reads (a directory for
// file and folder layouts and for a template's prefix, a file for a singleton
// or a collection), and the type then scans as every read scans it. A
// missing storage path is an empty type, not a finding.
func checkLayout(root string, resolved *schema.ResolvedSchema) []Check {
	var out []Check
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		if c, bad := layoutOf(root, resolved, rtype); bad {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []Check{{Name: "layout", Status: OK, Message: fmt.Sprintf(
			"Storage matches the layout of every type (%s)", plural(resolved.Types.Len(), "type"))}}
	}
	return out
}

func layoutOf(root string, resolved *schema.ResolvedSchema, rtype *schema.ResolvedType) (Check, bool) {
	wantFile := !rtype.Templated() &&
		(rtype.Storage.Layout == schema.LayoutCollection || rtype.Storage.Layout == schema.LayoutSingleton)
	for _, rel := range rtype.StorageRoots() {
		if rel == "" || resolved.Ignore.Ignored(rel, !wantFile) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := fsio.Stat(root, path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Check{Name: "layout", Status: Fail, Code: "layout_mismatch", Message: rtype.Name, Err: err}, true
		}
		layout := "the " + rtype.Storage.Layout + " layout"
		if rtype.Templated() {
			layout = "a path template"
		}
		switch {
		case wantFile && !info.Mode().IsRegular():
			return mismatch(rtype.Name, rel, layout, "a file"), true
		case !wantFile && !info.IsDir():
			return mismatch(rtype.Name, rel, layout, "a directory"), true
		}
	}
	if err := index.ScanOne(root, resolved, rtype); err != nil {
		return Check{Name: "layout", Status: Fail, Code: "layout_mismatch", Message: rtype.Name, Err: err}, true
	}
	return Check{}, false
}

func mismatch(typ, rel, layout, want string) Check {
	return Check{Name: "layout", Status: Fail, Code: "layout_mismatch", Message: fmt.Sprintf(
		"%s: %s is not %s, which %s needs; move it aside or change the type's storage path",
		typ, rel, want, layout)}
}

func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
