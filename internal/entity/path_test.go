package entity

// get --path: the path forms and refusals the parity case cannot spell. The
// runner hands khub no absolute workspace path, and a symlink fixture would
// need a Windows twin, so absolute paths, a root reached through a symlink,
// a symlink inside the workspace and the collection id cap live here.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// realWS is a workspace as the CLI hands it to the core: symlinks resolved.
func realWS(t *testing.T, preset string) string {
	t.Helper()
	ws, err := filepath.EvalSymlinks(newWS(t, preset))
	requireNoError(t, err)
	return ws
}

func getOneByPath(t *testing.T, ws, p string) string {
	t.Helper()
	views, err := GetManyByPath(ws, []string{p}, false)
	requireNoError(t, err)
	return views[0].Type + "/" + views[0].Slug
}

func TestGetByPathAbsolute(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	if got := getOneByPath(t, ws, filepath.Join(ws, "clients", "acme.md")); got != "client/acme" {
		t.Fatalf("got %s", got)
	}
	_, err := GetManyByPath(ws, []string{filepath.Join(filepath.Dir(ws), "elsewhere.md")}, false)
	_ = requireCode(t, err, "path_outside_workspace")
}

// An absolute path may spell the root through a symlink outside the
// workspace, as macOS /tmp spells /private/tmp.
func TestGetByPathRootThroughSymlink(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	link := filepath.Join(t.TempDir(), "ws-link")
	if err := os.Symlink(ws, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if got := getOneByPath(t, ws, filepath.Join(link, "clients", "acme.md")); got != "client/acme" {
		t.Fatalf("got %s", got)
	}
	// A symlink outside the workspace to a directory inside it is not the
	// root, so the path is outside.
	sub := filepath.Join(t.TempDir(), "clients-link")
	if err := os.Symlink(filepath.Join(ws, "clients"), sub); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	_, err := GetManyByPath(ws, []string{filepath.Join(sub, "acme.md")}, false)
	_ = requireCode(t, err, "path_outside_workspace")
}

// A symlink inside the workspace is refused the way storage refuses one, never
// followed to the entity it points at.
func TestGetByPathRefusesSymlinkInWorkspace(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	if err := os.Symlink("clients", filepath.Join(ws, "cl")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	_, err := GetManyByPath(ws, []string{"cl/acme.md"}, false)
	_ = requireCode(t, err, "unsafe_path")
}

// Any file under a folder entity's directory resolves to that entity, however
// deep; an entity file of another type wins over the folder it sits in.
func TestGetByPathFolderOwnsItsTree(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "partnerships/acme/_index.md", kv("type", "partnership", "partner", "Acme"))
	mkdirAll(t, filepath.Join(ws, "partnerships", "acme", "deck"))
	writeFile(t, filepath.Join(ws, "partnerships", "acme", "deck", "q3.txt"), "slides\n")
	for _, p := range []string{"partnerships/acme", "partnerships/acme/_index.md", "partnerships/acme/deck", "partnerships/acme/deck/q3.txt"} {
		if got := getOneByPath(t, ws, p); got != "partnership/acme" {
			t.Fatalf("%s resolved to %s", p, got)
		}
	}
	_, err := GetManyByPath(ws, []string{"partnerships/acme/deck/missing.txt"}, false)
	_ = requireCode(t, err, "path_not_entity")
}

// A collection file refuses with its ids, capped at ten, and names the calls
// that read one or list them all. An empty collection holds no entity.
func TestGetByPathCollection(t *testing.T) {
	ws, err := filepath.EvalSymlinks(newWSFrom(t, "preset-cells", filepath.Join(repoRoot(t), "parity", "corpus")))
	requireNoError(t, err)
	for i := range 12 {
		_, err := Create(ws, "repo", CreateOpts{Fields: fields("title", fmt.Sprintf("r%02d", i)), UseTemplate: true})
		requireNoError(t, err)
	}
	_, err = GetManyByPath(ws, []string{"repos.jsonl"}, false)
	e := requireCode(t, err, "path_is_collection")
	requireMessageContains(t, e, "'repos.jsonl' is a repo collection holding 12 entities: "+
		"repo/r00, repo/r01, repo/r02, repo/r03, repo/r04, repo/r05, repo/r06, repo/r07, repo/r08, repo/r09 and 2 more. "+
		"Get one by id (khub get <id>) or list them (khub query --type repo)")

	writeFile(t, filepath.Join(ws, "vendors.yaml"), "")
	_, err = GetManyByPath(ws, []string{"vendors.yaml"}, false)
	e = requireCode(t, err, "path_not_entity")
	requireMessageContains(t, e, "the vendor collection holds no entities")
}
