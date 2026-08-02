// Package cli wires the champs commands — apply, plan, version — around
// the reconcile engine. Everything user-facing goes to one stdout stream
// (DESIGN-0001); fatal errors go to stderr; the exit code is the CI
// contract: 0 completed (skips included), 1 on any error.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/champs/internal/config"
)

// EnvBaseURL overrides the GitHub API base URL. It exists for tests
// (pointing the real production path at a ghtest server) and doubles as
// a GHES escape hatch.
const EnvBaseURL = "CHAMPS_GITHUB_BASE_URL"

// ErrRunFailed marks a run whose errors are already in the rendered
// summary — Execute exits 1 without printing anything further.
var ErrRunFailed = errors.New("run completed with errors")

// BuildInfo is the ldflags metadata injected into main.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func (b BuildInfo) String() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", b.Version, b.Commit, b.Date)
}

// Execute runs the CLI and returns the process exit code.
func Execute(ctx context.Context, info BuildInfo) int {
	return execute(ctx, info, os.Args[1:], os.Stdout, os.Stderr)
}

// execute is the injectable core: tests pass buffers and assert the
// exit code.
func execute(ctx context.Context, info BuildInfo, args []string, out, errOut io.Writer) int {
	root := newRoot(info)
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(errOut)

	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrRunFailed):
		return 1
	default:
		printFatal(errOut, err)
		return 1
	}
}

// printFatal reports a fatal error on stderr. Config diagnostics get
// their full HCL rendering; everything else prints one line.
func printFatal(errOut io.Writer, err error) {
	if de, ok := errors.AsType[*config.DiagnosticsError](err); ok {
		if rerr := de.Render(errOut, 0, false); rerr == nil {
			return
		}
	}
	if _, werr := fmt.Fprintln(errOut, "Error:", err); werr != nil {
		return // best-effort: a failed stderr write at exit has no recovery
	}
}

func newRoot(info BuildInfo) *cobra.Command {
	root := &cobra.Command{
		Use:           "champs",
		Short:         "Reconcile per-org security-champions teams from a roster",
		Version:       info.String(),
		SilenceErrors: true, // execute owns error printing
	}
	root.SetVersionTemplate("champs {{.Version}}\n")
	root.AddCommand(newApplyCmd(), newPlanCmd(), newVersionCmd(info))
	return root
}

// colorEnabled decides output color: --no-color wins, then a non-empty
// NO_COLOR env var, then w must be a terminal — only checkable when w is
// an *os.File. Any other writer (pipes, test buffers) gets plain text,
// which is exactly the "no ANSI when piped" contract.
func colorEnabled(noColorFlag bool, w io.Writer) bool {
	if noColorFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
