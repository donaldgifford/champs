package config_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/donaldgifford/champs/internal/config"
)

// designExample is the DESIGN-0001 example config, verbatim.
const designExample = `team {
  slug        = "security_champions"
  description = "Security champions with existing access to this org"
  privacy     = "closed"
}

github {
  app_id           = 12345
  private_key_path = "/path/to/key.pem" # or CHAMPS_GITHUB_PRIVATE_KEY env var
}

org "org1" {}
org "org2" {}
org "org3" {}
`

func TestParseDesignExample(t *testing.T) {
	cfg, err := config.Parse([]byte(designExample), "champs.hcl")
	if err != nil {
		t.Fatalf("Parse(designExample) error = %v, want nil", err)
	}

	if got := cfg.Team.Slug; got != "security_champions" {
		t.Errorf("Team.Slug = %q, want %q", got, "security_champions")
	}
	if got := cfg.Team.Privacy; got != "closed" {
		t.Errorf("Team.Privacy = %q, want %q", got, "closed")
	}
	if cfg.Team.Description == "" {
		t.Error("Team.Description = empty, want the example description")
	}
	if got := cfg.GitHub.AppID; got != 12345 {
		t.Errorf("GitHub.AppID = %d, want 12345", got)
	}
	if got := cfg.GitHub.PrivateKeyPath; got != "/path/to/key.pem" {
		t.Errorf("GitHub.PrivateKeyPath = %q, want %q", got, "/path/to/key.pem")
	}
	if len(cfg.Orgs) != 3 {
		t.Fatalf("len(Orgs) = %d, want 3", len(cfg.Orgs))
	}
	for i, want := range []string{"org1", "org2", "org3"} {
		if got := cfg.Orgs[i].Name; got != want {
			t.Errorf("Orgs[%d].Name = %q, want %q", i, got, want)
		}
	}
}

func TestParsePrivacyDefaults(t *testing.T) {
	src := `team {
  slug = "security_champions"
}

github {
  app_id = 1
}

org "org1" {}
`
	cfg, err := config.Parse([]byte(src), "champs.hcl")
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if got := cfg.Team.Privacy; got != config.DefaultPrivacy {
		t.Errorf("Team.Privacy = %q, want default %q", got, config.DefaultPrivacy)
	}
}

func TestParseDiagnostics(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		wantMinDiags int
	}{
		{"syntax error", "team {\n", 1},
		{"missing required blocks", "org \"a\" {}\n", 1},
		{
			"aggregates multiple problems",
			designExample + "\nbogus_one = true\nbogus_two = true\n",
			2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Parse([]byte(tt.src), "champs.hcl")
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want diagnostics", tt.src)
			}

			var de *config.DiagnosticsError
			if !errors.As(err, &de) {
				t.Fatalf("Parse(%q) error type = %T, want *DiagnosticsError", tt.src, err)
			}
			if len(de.Diagnostics) < tt.wantMinDiags {
				t.Errorf("len(Diagnostics) = %d, want >= %d: %v",
					len(de.Diagnostics), tt.wantMinDiags, de)
			}

			var buf bytes.Buffer
			if err := de.Render(&buf, 78, false /* color */); err != nil {
				t.Fatalf("Render() error = %v, want nil", err)
			}
			if buf.Len() == 0 {
				t.Error("Render() wrote nothing, want a diagnostic report")
			}
		})
	}
}

// loaderFor returns a Loader that serves src as every file and env as the
// entire environment.
func loaderFor(src string, env map[string]string) config.Loader {
	return config.Loader{
		ReadFile: func(string) ([]byte, error) { return []byte(src), nil },
		LookupEnv: func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		},
	}
}

func TestLoaderLoadValidation(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		env     map[string]string
		wantIs  []error
		wantNil bool
	}{
		{
			name:    "design example is valid",
			src:     designExample,
			wantNil: true,
		},
		{
			name: "env key satisfies key source",
			src: `team {
  slug = "security_champions"
}
github {
  app_id = 1
}
org "a" {}
`,
			env:     map[string]string{config.EnvPrivateKey: "pem"},
			wantNil: true,
		},
		{
			name: "no orgs",
			src: `team {
  slug = "s"
}
github {
  app_id           = 1
  private_key_path = "k.pem"
}
`,
			wantIs: []error{config.ErrNoOrgs},
		},
		{
			name: "empty slug",
			src: `team {
  slug = ""
}
github {
  app_id           = 1
  private_key_path = "k.pem"
}
org "a" {}
`,
			wantIs: []error{config.ErrEmptyTeamSlug},
		},
		{
			name: "invalid privacy",
			src: `team {
  slug    = "s"
  privacy = "public"
}
github {
  app_id           = 1
  private_key_path = "k.pem"
}
org "a" {}
`,
			wantIs: []error{config.ErrInvalidPrivacy},
		},
		{
			name: "zero app id",
			src: `team {
  slug = "s"
}
github {
  app_id           = 0
  private_key_path = "k.pem"
}
org "a" {}
`,
			wantIs: []error{config.ErrMissingAppID},
		},
		{
			name: "no key source",
			src: `team {
  slug = "s"
}
github {
  app_id = 1
}
org "a" {}
`,
			wantIs: []error{config.ErrNoKeySource},
		},
		{
			name: "empty env value is not a key source",
			src: `team {
  slug = "s"
}
github {
  app_id = 1
}
org "a" {}
`,
			env:    map[string]string{config.EnvPrivateKey: ""},
			wantIs: []error{config.ErrNoKeySource},
		},
		{
			name: "duplicate org",
			src: `team {
  slug = "s"
}
github {
  app_id           = 1
  private_key_path = "k.pem"
}
org "a" {}
org "a" {}
`,
			wantIs: []error{config.ErrDuplicateOrg},
		},
		{
			name: "aggregates every problem",
			src: `team {
  slug = ""
}
github {
  app_id = 0
}
`,
			wantIs: []error{
				config.ErrEmptyTeamSlug,
				config.ErrMissingAppID,
				config.ErrNoKeySource,
				config.ErrNoOrgs,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loaderFor(tt.src, tt.env).Load("champs.hcl")

			if tt.wantNil {
				if err != nil {
					t.Fatalf("Load() error = %v, want nil", err)
				}
				if cfg == nil {
					t.Fatal("Load() config = nil, want non-nil")
				}
				return
			}

			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "invalid config") {
				t.Errorf("Load() error = %q, want it to mention invalid config", err)
			}
			for _, target := range tt.wantIs {
				if !errors.Is(err, target) {
					t.Errorf("Load() error = %v, want errors.Is %v", err, target)
				}
			}
		})
	}
}

func TestLoaderLoadReadError(t *testing.T) {
	l := config.Loader{
		ReadFile:  func(string) ([]byte, error) { return nil, os.ErrNotExist },
		LookupEnv: func(string) (string, bool) { return "", false },
	}

	_, err := l.Load("champs.hcl")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() error = %v, want errors.Is os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("Load() error = %q, want it to mention reading config", err)
	}
}
