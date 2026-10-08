// Package auth holds password hashing, tokens, OTP codes and the request principal.
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

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
