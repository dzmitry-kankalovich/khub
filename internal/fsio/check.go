// The dry-run halves of MkdirAll, publish and Remove: every refusal khub
// itself makes before the filesystem is touched, and no write.

package fsio

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
)

// CheckMkdirAll is MkdirAll without the mkdir: OpenPath's refusals, and a
// file standing where a directory must go.
func CheckMkdirAll(root, path string) error {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	part := ""
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		part = filepath.Join(part, name)
		info, err := r.Stat(part)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // MkdirAll creates the rest
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return &fs.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
		}
	}
	return nil
}

// CheckPublish is AtomicWriteIn (exclusive false) or WriteNewIn (exclusive
// true) up to the point publish creates its temporary: OpenPath's refusals,
// an existing file where a new one must go, and a non-regular file where one
// is replaced.
func CheckPublish(root, path string, exclusive bool) (err error) {
	defer func() { err = pathError(err, path) }()
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	info, err := r.Lstat(rel)
	switch {
	case err == nil && exclusive:
		return &fs.PathError{Op: "create", Path: path, Err: fs.ErrExist}
	case err == nil && !info.Mode().IsRegular():
		return &fs.PathError{Op: "write", Path: path, Err: fs.ErrInvalid}
	case err == nil, errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return err
}

// CheckRemove is Remove without the removal: OpenPath's refusals, and a path
// that is not there.
func CheckRemove(root, path string) error {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Lstat(rel); err != nil {
		return FileInPath(err, root, path)
	}
	return nil
}
