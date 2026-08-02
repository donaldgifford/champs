// Package config loads the champs HCL configuration.
//
// The configuration declares the managed organizations, the settings for the
// per-org security-champions team, and the GitHub App credentials used to
// mint installation tokens. The schema is defined in DESIGN-0001
// (docs/design/0001-champs-security-champions-team-management-cli.md).
package config

import (
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// DefaultPrivacy is the team privacy applied when team.privacy is omitted.
const DefaultPrivacy = "closed"

// Config is the root of the champs HCL configuration.
type Config struct {
	Team   Team   `hcl:"team,block"`
	GitHub GitHub `hcl:"github,block"`
	Orgs   []Org  `hcl:"org,block"`
}

// Team declares the security-champions team settings. Settings are applied
// only when champs creates the team; existing teams are never modified.
type Team struct {
	Slug        string `hcl:"slug"`
	Description string `hcl:"description,optional"`
	Privacy     string `hcl:"privacy,optional"`
}

// GitHub holds the GitHub App identity used to authenticate. The private key
// itself may instead come from the CHAMPS_GITHUB_PRIVATE_KEY environment
// variable, which wins over PrivateKeyPath.
type GitHub struct {
	AppID          int64  `hcl:"app_id"`
	PrivateKeyPath string `hcl:"private_key_path,optional"`
}

// Org names a managed organization.
type Org struct {
	Name string `hcl:"name,label"`
}

// Loader parses configuration with injectable OS dependencies so tests can
// substitute fakes for the filesystem and environment.
type Loader struct {
	// ReadFile reads a file's contents. Nil means os.ReadFile.
	ReadFile func(name string) ([]byte, error)
	// LookupEnv reports an environment variable. Nil means os.LookupEnv.
	LookupEnv func(key string) (string, bool)
}

// Load reads, parses, and validates the configuration file at path.
func (l Loader) Load(path string) (*Config, error) {
	src, err := l.readFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg, err := Parse(src, path)
	if err != nil {
		return nil, err
	}
	if err := l.validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func (l Loader) readFile(name string) ([]byte, error) {
	if l.ReadFile != nil {
		return l.ReadFile(name)
	}
	return os.ReadFile(name)
}

// Load reads, parses, and validates the configuration file at path using the
// real filesystem and environment.
func Load(path string) (*Config, error) {
	return Loader{}.Load(path)
}

// Parse parses src as champs HCL configuration without semantic validation;
// Load applies it. filename appears in diagnostics only. All syntax and
// decode problems are aggregated into a single *DiagnosticsError rather than
// returned one at a time.
func Parse(src []byte, filename string) (*Config, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)

	var cfg Config
	if file != nil && file.Body != nil {
		diags = append(diags, gohcl.DecodeBody(file.Body, nil, &cfg)...)
	}
	if diags.HasErrors() {
		return nil, &DiagnosticsError{Diagnostics: diags, files: parser.Files()}
	}

	if cfg.Team.Privacy == "" {
		cfg.Team.Privacy = DefaultPrivacy
	}
	return &cfg, nil
}
