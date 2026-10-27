package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// below are OWASP suggestions when using Argon2, especially this golang libary
// check the documentation here
// https://pkg.go.dev/golang.org/x/crypto/argon2
const (
	timeCost    = 3
	memoryCost  = 64 * 1024
	threads     = 4
	keyLength   = 32
	saltLength  = 32
	tokenLength = 32

	minPasswordLen = 12
	maxPasswordLen = 128
)

func generateSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// use this function for signups
func GenerateHashPassword(password []byte) (string, error) {
	salt, err := generateSalt()
	if err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	key := argon2.IDKey(password, salt, timeCost, memoryCost, threads, keyLength)
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		memoryCost, timeCost, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// use this function for logins
func ValidatePassword(givenPassword []byte, dbHash string) (bool, error) {
	m, t, p, salt, storedKey, err := parseHash(dbHash)
	if err != nil {
		return false, err
	}
	// so given a database hash string, generate an additional key using the input we were given
	testKey := argon2.IDKey(givenPassword, salt, t, m, p, uint32(len(storedKey)))
	// one hiccup is that unless we add some sort of throttling, an attacker can detect wrong passwords by how long the server takes to response
	// so by adding this suble.ConstantTimeCompare function no matter if a password is right or wrong, take the same amount of time.
	return subtle.ConstantTimeCompare(storedKey, testKey) == 1, nil
}

// argon2 uses some parameters, it's a cryptographic function that takes into account how powerful hardware is now a days
// m: memory, how much memory we'll use to calculate a hash
// t: time, how long it'll take due to iterating over the memory
// p: parallelism, how many threads to parse a hash.
// so given a dbHash string, return the parameters we used to generate that argon2 password
func parseHash(dbHash string) (m, t uint32, p uint8, salt, key []byte, err error) {
	var version int
	var rest string
	if _, err = fmt.Sscanf(dbHash, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s",
		&version, &m, &t, &p, &rest); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid hash format: %w", err)
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, errors.New("incompatible argon2 version")
	}
	saltB64, hashB64, ok := strings.Cut(rest, "$")
	if !ok {
		return 0, 0, 0, nil, nil, errors.New("invalid hash format")
	}
	if salt, err = base64.RawStdEncoding.DecodeString(saltB64); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("decoding salt: %w", err)
	}
	if key, err = base64.RawStdEncoding.DecodeString(hashB64); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("decoding hash: %w", err)
	}
	return m, t, p, salt, key, nil
}
