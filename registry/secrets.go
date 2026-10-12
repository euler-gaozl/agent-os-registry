package registry

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"regexp"
)

var secretName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,80}$`)

func secretCipher() (cipher.AEAD, error) {
	key := os.Getenv("MASTER_KEY")
	if len(key) < 32 {
		return nil, errors.New("MASTER_KEY must be configured to use encrypted credential storage")
	}
	sum := sha256.Sum256([]byte(key))
	block, e := aes.NewCipher(sum[:])
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}

func (s *Store) PutSecret(ctx context.Context, name, value string) error {
	if (!secretName.MatchString(name) || reservedCredentialName(name)) || value == "" || len(value) > 16000 {
		return errors.New("invalid credential name or value")
	}
	a, e := secretCipher()
	if e != nil {
		return e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	sealed := a.Seal(nonce, nonce, []byte(value), []byte(name))
	_, e = s.DB.Exec(ctx, "INSERT INTO secrets(name,sealed) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET sealed=$2,updated_at=now()", name, sealed)
	return e
}

func (s *Store) Secret(ctx context.Context, name string) (string, error) {
	if s.Remote != nil && s.Remote.RegistryURL != "" {
		return s.Remote.Secret(ctx, name)
	}
	if name == "" {
		return "", nil
	}
	if !secretName.MatchString(name) || reservedCredentialName(name) {
		return "", errors.New("invalid credential reference")
	}
	if v := credentialEnvironment(name); v != "" {
		return v, nil
	}
	a, e := secretCipher()
	if e != nil {
		return "", e
	}
	var sealed []byte
	if e = s.DB.QueryRow(ctx, "SELECT sealed FROM secrets WHERE name=$1", name).Scan(&sealed); e != nil {
		return "", errors.New("credential not configured: " + name)
	}
	if len(sealed) < a.NonceSize() {
		return "", errors.New("invalid sealed credential")
	}
	raw, e := a.Open(nil, sealed[:a.NonceSize()], sealed[a.NonceSize():], []byte(name))
	if e != nil {
		return "", errors.New("credential decryption failed")
	}
	return string(raw), nil
}

// Environment compatibility is deliberately limited to existing provider keys.
// Never expose arbitrary process environment variables through credential RPC.
func credentialEnvironment(name string) string {
	switch name {
	case "DEEPSEEK_API_KEY", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY":
		return os.Getenv(name)
	}
	return ""
}
func reservedCredentialName(name string) bool {
	switch name {
	case "MASTER_KEY", "DATABASE_URL", "MIGRATION_DATABASE_URL", "SERVICE_TOKEN", "SERVICE_PEERS", "ADMIN_TOKEN", "BUSINESS_TOKEN", "PGPASSWORD":
		return true
	}
	return false
}
