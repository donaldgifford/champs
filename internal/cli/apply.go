package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/champs/internal/config"
	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/reconcile"
	"github.com/donaldgifford/champs/internal/render"
	"github.com/donaldgifford/champs/internal/roster"
	"github.com/donaldgifford/champs/internal/stringset"
)

// runOptions carries the shared apply/plan flag values. A fresh instance
// is bound per command construction — no package-level flag state.
type runOptions struct {
	config      string
	roster      string
	orgs        []string
	prune       bool
	dryRun      bool
	noColor     bool
	parallelism int
}

// addRunFlags registers the flags apply and plan share. They are local
// flags, not persistent ones on the root, so version inherits nothing.
func addRunFlags(cmd *cobra.Command, o *runOptions) {
	f := cmd.Flags()
	f.StringVar(&o.config, "config", "champs.hcl", "path to HCL config")
	f.StringVar(&o.roster, "roster", "roster.csv", "path to roster CSV")
	f.StringSliceVar(&o.orgs, "orgs", nil,
		"limit the run to these configured orgs (repeatable or comma-separated)")
	f.BoolVar(&o.prune, "prune", false,
		"remove team members no longer on the roster")
	f.BoolVar(&o.noColor, "no-color", false, "disable colorized output")
	f.IntVar(&o.parallelism, "parallelism", reconcile.DefaultParallelism,
		"max concurrent org reconciles")
}

func newApplyCmd() *cobra.Command {
	o := &runOptions{}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Reconcile the security-champions team in every configured org",
		Args:  cobra.NoArgs,
		RunE:  o.runE,
	}
	addRunFlags(cmd, o)
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false,
		"compute the full diff but write nothing")
	return cmd
}

// newPlanCmd is a true alias for apply --dry-run: same runE, same flags,
// dry-run forced by construction rather than argument rewriting.
func newPlanCmd() *cobra.Command {
	o := &runOptions{dryRun: true}
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would change (apply --dry-run)",
		Args:  cobra.NoArgs,
		RunE:  o.runE,
	}
	addRunFlags(cmd, o)
	return cmd
}

func (o *runOptions) runE(cmd *cobra.Command, _ []string) error {
	// From here on errors are runtime, not usage — no help text.
	cmd.SilenceUsage = true
	out := cmd.OutOrStdout()

	cfg, err := config.Load(o.config)
	if err != nil {
		return err
	}
	orgs, err := validateOrgs(cfg, o.orgs)
	if err != nil {
		return err
	}
	logins, err := roster.Load(o.roster)
	if err != nil {
		return err
	}
	key, err := config.Loader{}.PrivateKey(cfg.GitHub)
	if err != nil {
		return err
	}

	var ghOpts []gh.Option
	if base := os.Getenv(EnvBaseURL); base != "" {
		ghOpts = append(ghOpts, gh.WithBaseURL(base))
	}
	app, err := gh.NewApp(cfg.GitHub.AppID, key, ghOpts...)
	if err != nil {
		return err
	}

	res, err := reconcile.Run(cmd.Context(), &reconcile.Options{
		Source: app,
		Team: gh.TeamSettings{
			Slug:        cfg.Team.Slug,
			Description: cfg.Team.Description,
			Privacy:     cfg.Team.Privacy,
		},
		Orgs:        orgs,
		Roster:      logins,
		Prune:       o.prune,
		DryRun:      o.dryRun,
		Parallelism: o.parallelism,
	})
	if err != nil {
		return err
	}

	r := render.Renderer{Out: out, Color: colorEnabled(o.noColor, out)}
	if err := r.Render(res); err != nil {
		return err
	}
	if res.HasErrors() {
		return ErrRunFailed
	}
	return nil
}

// validateOrgs returns the configured org names, or the --orgs subset of
// them. Any name not in the config is a hard error before any API call
// (DESIGN-0001 OQ-4) — the config is the naming authority.
func validateOrgs(cfg *config.Config, filter []string) ([]string, error) {
	configured := make([]string, 0, len(cfg.Orgs))
	known := make(stringset.Set, len(cfg.Orgs))
	for _, org := range cfg.Orgs {
		configured = append(configured, org.Name)
		known.Add(org.Name)
	}
	if len(filter) == 0 {
		return configured, nil
	}

	selected := make(stringset.Set, len(filter))
	var unknown []string
	for _, name := range filter {
		if !known.Contains(name) {
			unknown = append(unknown, strconv.Quote(name))
			continue
		}
		selected.Add(name)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown org(s) in --orgs: %s (configured: %s)",
			strings.Join(unknown, ", "), strings.Join(configured, ", "))
	}
	return selected.Sorted(), nil
}
