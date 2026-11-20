package secure

import (
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var ErrPasswordLength = errors.New("password must contain 14 to 72 bytes")

func VerifyPassword(hash, password string) bool {
	if len(password) > 72 {
		return false
	}
	present := hash != ""
	if !present {
		hash = "$2a$12$" + strings.Repeat("0", 53)
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return present && err == nil
}

func HashPassword(password string) (string, error) {
	if len(password) < 14 || len(password) > 72 {
		return "", ErrPasswordLength
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}
