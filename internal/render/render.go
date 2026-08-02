// Package render writes a reconcile result as the champs single stdout
// stream (DESIGN-0001 Output): Terraform-style diff blocks, skip records
// as slog JSON lines, then an end-of-run summary. The skip lines carry no
// timestamp so consecutive scheduled runs diff cleanly — they are the
// standing drift report.
package render

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/donaldgifford/champs/internal/reconcile"
)

const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

// Renderer writes one run's full output stream. Color is pure policy-in:
// the CLI decides (flag, NO_COLOR, TTY) — the renderer never inspects the
// environment or the writer.
type Renderer struct {
	Out   io.Writer
	Color bool
}

// Render writes diff blocks, then skip records, then the summary.
func (r Renderer) Render(res *reconcile.Result) error {
	p := &printer{w: r.Out}
	r.diff(p, res)
	if p.err != nil {
		return p.err
	}
	r.skips(res)
	r.summary(p, res)
	return p.err
}

// diff prints one block per org with changes or an error; adds are green,
// removals red. A clean no-op org prints nothing.
func (r Renderer) diff(p *printer, res *reconcile.Result) {
	for _, o := range res.Orgs {
		if len(o.Added) == 0 && len(o.Removed) == 0 && o.Err == nil {
			continue
		}
		p.printf("%s:\n", o.Org)
		for _, u := range o.Added {
			p.printf("%s\n", r.green("  + "+u))
		}
		for _, u := range o.Removed {
			p.printf("%s\n", r.red("  - "+u))
		}
		if o.Err != nil {
			p.printf("%s\n", r.red("  ! "+o.Err.Error()))
		}
		p.printf("\n")
	}
}

// skips emits one slog JSON line per skip record on the same stream,
// never colorized. The time attr is stripped so the block is diffable
// between runs; slog's handler contract swallows writer errors, which is
// acceptable for these advisory records.
func (r Renderer) skips(res *reconcile.Result) {
	logger := slog.New(slog.NewJSONHandler(r.Out, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	for _, o := range res.Orgs {
		for _, s := range o.Skips {
			if s.Reason == reconcile.SkipNoInstallation {
				logger.Warn("skip", "org", s.Org, "reason", string(s.Reason))
				continue
			}
			logger.Info("skip", "user", s.User, "org", s.Org, "reason", string(s.Reason))
		}
	}
	for _, u := range res.UnknownUsers {
		logger.Info("skip", "user", u, "org", "", "reason", string(reconcile.SkipUnknownUser))
	}
}

// summary prints per-org counts (an errored org shows its error instead —
// partial progress is already in its diff block), residue-check failures,
// and the run totals in plan or apply wording.
func (r Renderer) summary(p *printer, res *reconcile.Result) {
	p.printf("%s\n", r.bold("Summary:"))
	for _, o := range res.Orgs {
		if o.Err != nil {
			p.printf("  %s: %s\n", o.Org, r.red("error: "+o.Err.Error()))
			continue
		}
		p.printf("  %s: %d added, %d removed, %d skipped\n",
			o.Org, len(o.Added), len(o.Removed), len(o.Skips))
	}
	for _, err := range res.ResidueErrs {
		p.printf("  %s\n", r.red("residue check error: "+err.Error()))
	}

	t := res.Totals()
	var errs string
	if t.Errors > 0 {
		errs = fmt.Sprintf(", %d error(s)", t.Errors)
	}
	if res.DryRun {
		p.printf("%s\n", r.bold(fmt.Sprintf(
			"Plan: %d to add, %d to remove, %d skipped%s.",
			t.Added, t.Removed, t.Skipped, errs)))
		return
	}
	p.printf("%s\n", r.bold(fmt.Sprintf(
		"Applied: %d added, %d removed, %d skipped%s.",
		t.Added, t.Removed, t.Skipped, errs)))
}

func (r Renderer) green(s string) string { return r.wrap(ansiGreen, s) }
func (r Renderer) red(s string) string   { return r.wrap(ansiRed, s) }
func (r Renderer) bold(s string) string  { return r.wrap(ansiBold, s) }

func (r Renderer) wrap(code, s string) string {
	if !r.Color {
		return s
	}
	return code + s + ansiReset
}

// printer latches the first write error so rendering code stays linear.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}
