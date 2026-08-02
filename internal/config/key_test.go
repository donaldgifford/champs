package config_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/donaldgifford/champs/internal/config"
)

const validPEM = "-----BEGIN TEST KEY-----\ndGVzdA==\n-----END TEST KEY-----\n"

func TestPrivateKey(t *testing.T) {
	const otherPEM = "-----BEGIN OTHER KEY-----\nb3RoZXI=\n-----END OTHER KEY-----\n"

	tests := []struct {
		name     string
		github   config.GitHub
		env      map[string]string
		files    map[string]string
		want     string
		wantIs   error
		wantMsgs []string
	}{
		{
			name:   "env wins over path",
			github: config.GitHub{PrivateKeyPath: "disk.pem"},
			env:    map[string]string{config.EnvPrivateKey: validPEM},
			files:  map[string]string{"disk.pem": otherPEM},
			want:   validPEM,
		},
		{
			name:     "env invalid pem names the env var",
			env:      map[string]string{config.EnvPrivateKey: "not a key"},
			wantIs:   config.ErrInvalidPEM,
			wantMsgs: []string{config.EnvPrivateKey},
		},
		{
			name:   "empty env falls through to file",
			github: config.GitHub{PrivateKeyPath: "disk.pem"},
			env:    map[string]string{config.EnvPrivateKey: ""},
			files:  map[string]string{"disk.pem": validPEM},
			want:   validPEM,
		},
		{
			name:   "file source",
			github: config.GitHub{PrivateKeyPath: "disk.pem"},
			files:  map[string]string{"disk.pem": validPEM},
			want:   validPEM,
		},
		{
			name:     "file invalid pem names the path",
			github:   config.GitHub{PrivateKeyPath: "disk.pem"},
			files:    map[string]string{"disk.pem": "garbage"},
			wantIs:   config.ErrInvalidPEM,
			wantMsgs: []string{"disk.pem"},
		},
		{
			name:     "file read error wrapped",
			github:   config.GitHub{PrivateKeyPath: "absent.pem"},
			wantIs:   os.ErrNotExist,
			wantMsgs: []string{"reading private key"},
		},
		{
			name:   "no source at all",
			wantIs: config.ErrNoKeySource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := config.Loader{
				LookupEnv: func(key string) (string, bool) {
					v, ok := tt.env[key]
					return v, ok
				},
				ReadFile: func(name string) ([]byte, error) {
					if content, ok := tt.files[name]; ok {
						return []byte(content), nil
					}
					return nil, os.ErrNotExist
				},
			}

			got, err := l.PrivateKey(tt.github)

			if tt.wantIs != nil {
				if !errors.Is(err, tt.wantIs) {
					t.Fatalf("PrivateKey() error = %v, want errors.Is %v", err, tt.wantIs)
				}
				for _, want := range tt.wantMsgs {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("PrivateKey() error = %q, want it to contain %q", err, want)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("PrivateKey() error = %v, want nil", err)
			}
			if string(got) != tt.want {
				t.Errorf("PrivateKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
