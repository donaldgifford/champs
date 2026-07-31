package config

import (
	"errors"
	"fmt"
	"os"
)

// EnvPrivateKey is the environment variable that provides the GitHub App
// private key as PEM contents. When set and non-empty it wins over
// github.private_key_path.
const EnvPrivateKey = "CHAMPS_GITHUB_PRIVATE_KEY"

// validate applies the semantic checks HCL decoding cannot express. Every
// problem is collected into one error so a single run reports them all;
// individual causes remain matchable with errors.Is.
func (l Loader) validate(cfg *Config) error {
	var errs []error

	if cfg.Team.Slug == "" {
		errs = append(errs, fmt.Errorf("team.slug: %w", ErrEmptyTeamSlug))
	}
	switch cfg.Team.Privacy {
	case "closed", "secret":
	default:
		errs = append(errs, fmt.Errorf("team.privacy %q (want \"closed\" or \"secret\"): %w",
			cfg.Team.Privacy, ErrInvalidPrivacy))
	}
	if cfg.GitHub.AppID == 0 {
		errs = append(errs, fmt.Errorf("github.app_id: %w", ErrMissingAppID))
	}
	if v, ok := l.lookupEnv(EnvPrivateKey); (!ok || v == "") && cfg.GitHub.PrivateKeyPath == "" {
		errs = append(errs, fmt.Errorf("github.private_key_path is empty and %s is unset: %w",
			EnvPrivateKey, ErrNoKeySource))
	}
	if len(cfg.Orgs) == 0 {
		errs = append(errs, ErrNoOrgs)
	}
	seen := make(map[string]struct{}, len(cfg.Orgs))
	for _, org := range cfg.Orgs {
		if _, dup := seen[org.Name]; dup {
			errs = append(errs, fmt.Errorf("org %q: %w", org.Name, ErrDuplicateOrg))
		}
		seen[org.Name] = struct{}{}
	}

	return errors.Join(errs...)
}

func (l Loader) lookupEnv(key string) (string, bool) {
	if l.LookupEnv != nil {
		return l.LookupEnv(key)
	}
	return os.LookupEnv(key)
}
