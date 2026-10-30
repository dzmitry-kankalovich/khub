// Dead relative links in bodies — the remainder of the body-assertions port
// (issue #52). A body that links `../decisions/use-postgres.md` after that
// file was renamed is still the document it claims to be, with a broken
// pointer in it: a gap, like a thin section, reported and never gating.

package integrity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/mdlink"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
)

// deadLinkGaps reads the body of every md entity with a file of its own (a
// collection row has none to be relative to) inside target, and reports each
// distinct relative link whose file does not exist as a `body` gap. Nodes in
// sorted order, links in order of first appearance. Fenced code, HTML
// comments and code spans are not read: a link written there is an example.
func deadLinkGaps(
	root string, resolved *schema.ResolvedSchema, valid *index.Index, target *string,
) []FieldError {
	gaps := []FieldError{}
	names := dirNames{}
	for _, node := range sortedNodes(valid) {
		if !inTarget(node, target) {
			continue
		}
		rtype, _ := resolved.Types.Get(node.Type)
		if !rtype.ReadsTemplate() { // md and not a collection: a body file
			continue
		}
		path := entity.EntityPath(root, valid, node)
		body, ok := readBody(path)
		if !ok {
			continue // unreadable bodies are reported by the shape check, if anywhere
		}
		for _, link := range mdlink.RelativeLinks(template.Prose(body)) {
			resolvedPath := filepath.Join(filepath.Dir(path), filepath.FromSlash(link.Path))
			if names.exists(root, resolvedPath) {
				continue
			}
			gaps = append(gaps, FieldError{
				Type: node.Type, Slug: node.Slug, Field: "body",
				Reason: fmt.Sprintf("dead link '%s' (no such file)", link.Dest),
			})
		}
	}
	return gaps
}

// dirNames caches directory listings for exact-case matching.
type dirNames map[string]map[string]bool

// exists reports whether path names a file or directory, with every
// component below root spelled exactly as on disk. A case-insensitive
// filesystem (macOS, Windows) would otherwise call `Decision.md` live while
// the link is dead on Linux, in git, and on every forge that renders it — and
// the same workspace would answer differently by platform. A path outside
// root is only checked for existence.
func (d dirNames) exists(root, path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	dir := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if !d.has(dir, part) {
			return false
		}
		dir = filepath.Join(dir, part)
	}
	return true
}

func (d dirNames) has(dir, name string) bool {
	set, ok := d[dir]
	if !ok {
		set = map[string]bool{}
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				set[e.Name()] = true
			}
		}
		d[dir] = set
	}
	return set[name]
}
