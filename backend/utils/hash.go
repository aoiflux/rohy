// Package utils holds small, dependency-light helpers shared across the backend:
// content hashing today, streaming/backpressure primitives later. It sits below
// the domain packages and must not import ingestion, persistence, or the API.
package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"rohy/backend/consts"
)

// HashBytes returns the lowercase hex SHA-256 digest of b. It is the single
// hashing primitive used for both hash_raw and hash_normalized so the algorithm
// (consts.HashAlgorithm) is defined in exactly one place.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashString is the string convenience form of HashBytes, used for the raw event
// payload digest (hash_raw).
func HashString(s string) string {
	return HashBytes([]byte(s))
}

// HashFields builds an order-stable digest over a set of normalized scalar fields
// (hash_normalized). Fields are joined with consts.FieldSeparator — a control
// character that cannot appear in event text — so distinct field boundaries can
// never collide by concatenation.
func HashFields(fields ...string) string {
	return HashString(strings.Join(fields, consts.FieldSeparator))
}
