package config

import (
	"encoding/pem"
	"fmt"
)

// PrivateKey resolves the GitHub App private key as PEM bytes. The
// CHAMPS_GITHUB_PRIVATE_KEY environment variable wins when set and non-empty
// (so CI never writes the secret to disk); otherwise the file at
// github.private_key_path is read. Both sources are sanity-checked as PEM so
// a mispasted secret fails here with a clear source, not later inside JWT
// signing.
func (l Loader) PrivateKey(g GitHub) ([]byte, error) {
	if v, ok := l.lookupEnv(EnvPrivateKey); ok && v != "" {
		key := []byte(v)
		if err := checkPEM(key); err != nil {
			return nil, fmt.Errorf("%s: %w", EnvPrivateKey, err)
		}
		return key, nil
	}

	if g.PrivateKeyPath == "" {
		return nil, ErrNoKeySource
	}
	key, err := l.readFile(g.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key: %w", err)
	}
	if err := checkPEM(key); err != nil {
		return nil, fmt.Errorf("private key %s: %w", g.PrivateKeyPath, err)
	}
	return key, nil
}

func checkPEM(data []byte) error {
	if block, _ := pem.Decode(data); block == nil {
		return ErrInvalidPEM
	}
	return nil
}
