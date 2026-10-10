// Package auth holds password hashing, tokens, OTP codes and the request principal.
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// MinPasswordLen is the shortest password accepted for web logins (API and CLI).
const MinPasswordLen = 8

// MaxPasswordBytes: bcrypt uses at most 72 bytes and refuses longer input, so
// longer passwords are rejected as invalid input (not a server error).
const MaxPasswordBytes = 72

// PasswordProblem returns why a new password is not acceptable, or "".
func PasswordProblem(p string) string {
	switch {
	case len(p) < MinPasswordLen:
		return "must be at least 8 characters"
	case len(p) > MaxPasswordBytes:
		return "must be at most 72 bytes (about 72 letters or digits)"
	}
	return ""
}

func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	return string(h), err
}

// CheckPassword reports whether plain matches hash. A nil hash never matches.
func CheckPassword(hash *string, plain string) bool {
	if hash == nil {
		// Spend the same time as a real check so missing accounts are not detectable by timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(plain))
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(*hash), []byte(plain))
	return err == nil
}

var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcryptCost)
	if err != nil {
		panic(errors.New("bcrypt init failed"))
	}
	return h
}()
