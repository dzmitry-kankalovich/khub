// warnings.go: the session-scoped integrity warnings a write verb prints to
// stderr. The logic is integrity.SessionWarnings; this file only names the
// session and prints the lines.

package cli

import (
	"fmt"
	"os"

	"github.com/endgame-build/khub/internal/integrity"
)

// writeSession names the session a write belongs to. KHUB_SESSION wins, and
// set but empty turns the warnings off. Without it, Claude Code's
// CLAUDE_CODE_SESSION_ID, which it exports to every shell it runs, so an agent
// gets the warnings without having to ask. Neither set: no session, no
// warnings, and a write prints exactly what it always has.
func writeSession() (string, bool) {
	if s, set := os.LookupEnv("KHUB_SESSION"); set {
		return s, s != ""
	}
	s := os.Getenv("CLAUDE_CODE_SESSION_ID")
	return s, s != ""
}

// warnAfterWrite prints `warning: <id> › <rule>: <message>` for each gating
// `check` finding this session has not been shown. A write verb calls it after
// the write landed and before Emit, so the lines precede the result as a
// search note does. Advisory: a failure here prints nothing and never changes
// the write's exit code.
func warnAfterWrite(ws string) {
	session, ok := writeSession()
	if !ok {
		return
	}
	findings, err := integrity.SessionWarnings(ws, session)
	if err != nil {
		return
	}
	for _, f := range findings {
		fmt.Fprintln(os.Stderr, "warning: "+f.Line())
	}
}
