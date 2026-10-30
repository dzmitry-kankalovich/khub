// completion.go adapts `khub completion <shell>`: print the shell script that
// teaches bash, zsh, fish or PowerShell to complete khub's commands and
// options. The script itself is cobra's; at TAB time it calls back into the
// binary as `khub __complete <words…>`, so the candidates always match the
// binary that answers, never a list frozen into the script.
//
// It is khub's own command rather than cobra's default one. Cobra's default is
// a group with one subcommand per shell, each with cobra-styled help and a
// --no-descriptions flag; khub's surface is Click-shaped, so the shell is a
// positional argument checked like any other, and the help is the Rich panel
// every other command draws.

package cli

import (
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// completionShells is the closed set, in the order help and errors list it.
var completionShells = []string{"bash", "zsh", "fish", "powershell"}

const completionArgSpec = "SHELL"

func registerCompletion(root *cobra.Command) {
	cmd := newCmd("completion",
		"Print a shell completion script: khub completion bash|zsh|fish|powershell.",
		completionArgSpec, func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), completionArgSpec, completionArgSpec)
			}
			shell := args[0]
			if !isCompletionShell(shell) {
				quoted := make([]string, len(completionShells))
				for i, s := range completionShells {
					quoted[i] = "'" + s + "'"
				}
				return clickUsageErr(cmd.CommandPath(), completionArgSpec,
					"Invalid value for '"+completionArgSpec+"': '"+shell+"' is not one of "+
						strings.Join(quoted, ", ")+".")
			}
			return Guard("text", func() error {
				return writeCompletion(cmd.Root(), shell, os.Stdout)
			})
		})
	cmd.Long = "Print a shell completion script: khub completion bash|zsh|fish|powershell.\n\n" +
		"Load it for this session with `source <(khub completion zsh)`, or write it to " +
		"the shell's completion directory to keep it. Candidates come from the binary at TAB time."
	cmd.Args = clickArity(1)
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completionShells, cobra.ShellCompDirectiveNoFileComp
	}
	root.AddCommand(cmd)
}

func isCompletionShell(shell string) bool {
	for _, s := range completionShells {
		if s == shell {
			return true
		}
	}
	return false
}

// writeCompletion emits cobra's script for one shell, with descriptions: the
// candidates carry each command's one-line help. A user who wants them off
// sets KHUB_COMPLETION_DESCRIPTIONS=false, which cobra reads at TAB time.
func writeCompletion(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	default:
		return root.GenPowerShellCompletionWithDesc(w)
	}
}

// isCompletionRequest reports cobra's hidden callback, `khub __complete …`,
// which the generated scripts run at TAB time.
func isCompletionRequest(rest []string) bool {
	return len(rest) > 0 &&
		(rest[0] == cobra.ShellCompRequestCmd || rest[0] == cobra.ShellCompNoDescRequestCmd)
}

// skipBlankLines drops the empty candidate cobra emits for khub's nameless
// help command. Cobra offers the help command at the root whatever its
// visibility, and khub's has the empty name (Click has no `help` command; see
// newRoot), so without this every root TAB would offer an empty word. Cobra
// writes one candidate per Write call, and no real candidate is empty.
type skipBlankLines struct{ w io.Writer }

func (s skipBlankLines) Write(p []byte) (int, error) {
	if string(p) == "\n" {
		return len(p), nil
	}
	return s.w.Write(p)
}
