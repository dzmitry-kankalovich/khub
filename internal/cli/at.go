// at.go is the adapter half of the point-in-time reads: the global `--at
// <ref|date>` runs a read command against the workspace as a git commit holds
// it, and `--as-of <date>` pins the date the stale flag is judged against.
// The snapshot itself is internal/gitlog's; this file only scopes it to one
// invocation and keeps the writes away from it.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/gitlog"
)

// snapshot is the --at tree for this invocation, materialized on the first
// resolveRoot and removed when Execute returns.
var snapshot *gitlog.Snapshot

// readsAt lists the commands that run under --at: every one that only reads
// the workspace. Anything else writes (add, edit, reindex, schema snapshot …),
// serves the live tree (serve), or writes a file the scratch tree would
// swallow (viz).
var readsAt = map[string]bool{
	"khub status":       true,
	"khub get":          true,
	"khub query":        true,
	"khub search":       true,
	"khub neighbors":    true,
	"khub impact":       true,
	"khub history":      true,
	"khub validate":     true,
	"khub check":        true,
	"khub stale":        true,
	"khub schema":       true,
	"khub schema types": true,
	"khub schema show":  true,
	"khub schema base":  true,
	"khub schema edges": true,
	"khub schema diff":  true,
}

// refuseWritesAt is the root's PersistentPreRunE: under --at, a command
// outside readsAt refuses with read_only_at before anything runs.
func refuseWritesAt(cmd *cobra.Command, _ []string) error {
	if state.At == "" || readsAt[cmd.CommandPath()] {
		return nil
	}
	format := ""
	if f := cmd.Flags().Lookup("format"); f != nil {
		format = f.Value.String()
	}
	e := errs.ReadOnlyAt(cmd.CommandPath())
	return Fail(e.Message, e.Code, format)
}

// snapshotRoot materializes the --at commit once per invocation. The live
// workspace is found exactly as without --at (-C and the walk up to .khub/),
// and the snapshot takes the same directory within the repository.
func snapshotRoot() (string, error) {
	if snapshot != nil {
		return snapshot.Root, nil
	}
	live, err := liveRoot()
	if err != nil {
		return "", err
	}
	snap, err := gitlog.Materialize(live, state.At)
	if err != nil {
		return "", err
	}
	snapshot = snap
	return snap.Root, nil
}

// closeSnapshot removes the scratch tree, if one was made.
func closeSnapshot() {
	if snapshot != nil {
		_ = snapshot.Close()
		snapshot = nil
	}
}

// history is where `stale` reads commit dates: the live HEAD, or under --at
// the live repository up to the snapshot's commit.
func history(ws string) gitlog.History {
	if snapshot != nil {
		return snapshot.History()
	}
	return gitlog.Head(ws)
}
