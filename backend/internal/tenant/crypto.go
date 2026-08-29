package tenant

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptHash hashes a password using bcrypt at the default cost.
// Used in production via NewService.
func bcryptHash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// bcryptCheck compares a bcrypt hash with a plaintext password.
func bcryptCheck(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// fakeHash is a trivial reversible "hash" used in unit tests to avoid bcrypt latency.
func fakeHash(password string) (string, error) {
	return "fake:" + password, nil
}

// fakeCheck is the inverse of fakeHash.
func fakeCheck(hash, password string) error {
	if hash != "fake:"+password {
		return fmt.Errorf("password mismatch")
	}
	return nil
}
