// snapshot.go is the point-in-time read behind the global `--at <ref|date>`:
// the workspace's tree as one commit holds it, written to a scratch directory
// every read verb can run against unchanged. The graph is derived, never
// stored, so a past commit's tree is all a past projection needs.
//
// Every git call here is read-only, like the rest of the package: the live
// repository, its index and its working tree are never touched.

package gitlog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/errs"
)

// Snapshot is a workspace materialized from one commit.
type Snapshot struct {
	Root   string // the scratch workspace root the read verbs run against
	Commit string // the full commit id the tree came from
	live   string // the live workspace root, where git runs
}

// History is where a snapshot's commit dates come from: the live repository,
// read up to the snapshot's commit. Relative paths agree, since Root mirrors
// the live workspace directory.
func (s *Snapshot) History() History { return History{Dir: s.live, Rev: s.Commit} }

// Close removes the scratch directory.
func (s *Snapshot) Close() error { return os.RemoveAll(s.Root) }

var reAtDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// AtDate reports whether an `--at` value is a date, YYYY-MM-DD. A date wins
// over a ref of the same spelling; `refs/tags/<name>` names the ref.
func AtDate(spec string) (time.Time, bool) {
	if !reAtDate.MatchString(spec) {
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", spec, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

// ResolveAt turns an `--at` value into a full commit id. A date is the last
// commit on HEAD's first-parent line committed at or before the end of that
// day, UTC; anything else is a git revision (commit, branch, tag, HEAD~3).
func ResolveAt(root, spec string) (string, error) {
	ok, err := hasHistory(Head(root))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errs.NoGitHistory()
	}
	if d, isDate := AtDate(spec); isDate {
		before := d.Format("2006-01-02") + " 23:59:59 +0000"
		out, code, err := run(root, "rev-list", "-1", "--first-parent", "--before="+before, "HEAD")
		if err != nil {
			return "", err
		}
		commit := strings.TrimSpace(out)
		if code != 0 || commit == "" {
			return "", errs.NoCommitBefore(spec)
		}
		return commit, nil
	}
	out, code, err := run(root, "rev-parse", "--verify", "--quiet", "--end-of-options", spec+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(out)
	if code != 0 || commit == "" {
		return "", errs.UnknownRevision(spec)
	}
	return commit, nil
}

// Materialize writes the workspace directory's tree at the commit `spec`
// names into a fresh scratch directory. root is the live workspace root; the
// snapshot takes the same directory within the repository, so a workspace
// nested in a larger repository is read from the same place.
//
// The tree is read blob by blob (`ls-tree` + `cat-file --batch`), not through
// `git archive`, so export-ignore and export-subst attributes cannot drop or
// rewrite a file. Blobs are written as stored: no smudge filter, no line-ending
// conversion. A submodule becomes an empty directory, as a checkout without
// `--recurse-submodules` leaves it. Every write goes through an os.Root, so a
// crafted tree cannot place a file outside the scratch directory.
func Materialize(root, spec string) (snap *Snapshot, err error) {
	commit, err := ResolveAt(root, spec)
	if err != nil {
		return nil, err
	}
	prefixOut, code, err := run(root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errs.NoGitHistory()
	}
	prefix := strings.TrimSpace(prefixOut)
	if _, code, err := run(root, "cat-file", "-e", commit+":"+prefix+".khub"); err != nil {
		return nil, err
	} else if code != 0 {
		return nil, errs.NoWorkspaceAt(spec)
	}
	listing, code, err := run(root, "ls-tree", "-r", "-z", "--full-tree", commit+":"+prefix)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errs.NoWorkspaceAt(spec)
	}
	entries, err := parseTree(listing)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "khub-at-")
	if err != nil {
		return nil, err
	}
	snap = &Snapshot{Root: dir, Commit: commit, live: root}
	defer func() {
		if err != nil {
			_ = snap.Close()
			snap = nil
		}
	}()
	scratch, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = scratch.Close() }()
	if err := writeTree(root, scratch, entries); err != nil {
		return nil, err
	}
	return snap, nil
}

// treeEntry is one `ls-tree -r` line.
type treeEntry struct {
	mode, kind, oid, path string
}

// parseTree reads `ls-tree -r -z` output: `<mode> <type> <oid>\t<path>\0`.
func parseTree(listing string) ([]treeEntry, error) {
	var out []treeEntry
	for _, line := range strings.Split(listing, "\x00") {
		if line == "" {
			continue
		}
		head, p, found := strings.Cut(line, "\t")
		fields := strings.Fields(head)
		if !found || len(fields) != 3 {
			return nil, fmt.Errorf("unexpected git ls-tree line: %q", line)
		}
		if !safeTreePath(p) {
			return nil, fmt.Errorf("refusing tree path %q", p)
		}
		out = append(out, treeEntry{mode: fields[0], kind: fields[1], oid: fields[2], path: p})
	}
	return out, nil
}

// safeTreePath accepts a relative slash path with no empty, `.`, `..` or
// `.git` component: what a checkout would accept.
func safeTreePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		switch strings.ToLower(part) {
		case "", ".", "..", ".git":
			return false
		}
	}
	return true
}

// writeTree streams every blob through one `git cat-file --batch` and writes
// it under scratch.
func writeTree(gitDir string, scratch *os.Root, entries []treeEntry) error {
	var blobs []treeEntry
	for _, e := range entries {
		switch e.kind {
		case "blob":
			blobs = append(blobs, e)
		case "commit": // a submodule: an empty directory, as a plain checkout leaves it
			if err := scratch.MkdirAll(e.path, 0o755); err != nil {
				return err
			}
		}
	}
	if len(blobs) == 0 {
		return nil
	}
	var stdin bytes.Buffer
	for _, b := range blobs {
		stdin.WriteString(b.oid + "\n")
	}
	cmd := exec.Command("git", "-C", gitDir, "cat-file", "--batch")
	cmd.Stdin = &stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := readBatch(bufio.NewReader(stdout), scratch, blobs)
	if readErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return fmt.Errorf("git cat-file failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// readBatch reads one `<oid> <type> <size>\n<content>\n` record per blob, in
// the order they were asked for.
func readBatch(r *bufio.Reader, scratch *os.Root, blobs []treeEntry) error {
	for _, b := range blobs {
		header, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			return fmt.Errorf("unexpected git cat-file header: %q", strings.TrimSpace(header))
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return err
		}
		content := make([]byte, size)
		if _, err := io.ReadFull(r, content); err != nil {
			return err
		}
		if _, err := r.Discard(1); err != nil { // the record's trailing newline
			return err
		}
		if err := writeBlob(scratch, b, content); err != nil {
			return err
		}
	}
	return nil
}

// writeBlob writes one blob at its tree path. A symlink is recreated as one,
// so khub refuses it exactly as it would in a checkout; where the platform
// cannot create it, the link text is written as a file, which is what git
// itself does with core.symlinks=false.
func writeBlob(scratch *os.Root, b treeEntry, content []byte) error {
	if dir := path.Dir(b.path); dir != "." {
		if err := scratch.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	perm := fs.FileMode(0o644)
	switch b.mode {
	case "120000":
		if err := scratch.Symlink(string(content), b.path); err == nil {
			return nil
		} else if errors.Is(err, fs.ErrExist) {
			return err
		}
	case "100755":
		perm = 0o755
	}
	return scratch.WriteFile(b.path, content, perm)
}
