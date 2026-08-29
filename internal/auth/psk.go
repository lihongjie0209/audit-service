package auth

import (
	"crypto/subtle"
	"strings"
)

func VerifyPSK(authorization, expected string) bool {
	scheme, supplied, ok := strings.Cut(authorization, " ")
	if !ok || !strings.EqualFold(scheme, "PSK") || supplied == "" || len(supplied) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}
