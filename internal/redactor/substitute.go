package redactor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

// CalculateLuhnCheckDigit calculates the check digit that makes the number Luhn-valid.
func CalculateLuhnCheckDigit(digits []int) int {
	sum := 0
	double := true
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return (10 - (sum % 10)) % 10
}

// SubstituteCreditCard replaces a Luhn-valid credit card with a synthetic Luhn-valid test card
// on a standard test BIN while preserving length and separators (spaces or dashes).
func SubstituteCreditCard(raw string) string {
	if !IsValidLuhn(raw) {
		return raw
	}

	clean := CleanDigits(raw)
	n := len(clean)
	if n < 13 || n > 19 {
		return raw
	}

	// Select test BIN based on primary card network prefix
	bin := "424242" // Visa test BIN default
	switch clean[0] {
	case '5':
		bin = "555555" // Mastercard test BIN
	case '3':
		bin = "378282" // Amex test BIN
	case '6':
		bin = "601100" // Discover test BIN
	}

	if len(bin) >= n {
		bin = "42"
	}

	digits := make([]int, n-1)
	for i := 0; i < len(bin) && i < n-1; i++ {
		digits[i] = int(bin[i] - '0')
	}
	for i := len(bin); i < n-1; i++ {
		if i%2 == 0 {
			digits[i] = 4
		} else {
			digits[i] = 2
		}
	}

	checkDigit := CalculateLuhnCheckDigit(digits)
	fullDigits := append(digits, checkDigit)

	// Reconstruct with original formatting
	var b strings.Builder
	digitIdx := 0
	for _, r := range raw {
		if unicode.IsDigit(r) {
			if digitIdx < len(fullDigits) {
				b.WriteString(fmt.Sprintf("%d", fullDigits[digitIdx]))
				digitIdx++
			}
		} else {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// SubstituteEmail replaces an email address with a synthetic email on an RFC 2606 reserved domain.
func SubstituteEmail(raw string) string {
	parts := strings.SplitN(raw, "@", 2)
	if len(parts) != 2 {
		return raw
	}
	userPart := parts[0]

	// Hash user part to produce deterministic pseudo-name
	h := sha256.Sum256([]byte(userPart))
	hashHex := hex.EncodeToString(h[:])

	desiredLen := len(userPart)
	if desiredLen < 4 {
		desiredLen = 4
	}
	if desiredLen > 12 {
		desiredLen = 12
	}

	synthUser := "user_" + hashHex[:desiredLen-len("user_")+3]
	if len(synthUser) < 5 {
		synthUser = "user_001"
	}

	return synthUser + "@example.com"
}

// SubstituteToken replaces an API key or token with a synthetic value of identical length and prefix.
func SubstituteToken(raw string, prefix string) string {
	if !strings.HasPrefix(raw, prefix) {
		return raw
	}
	suffixLen := len(raw) - len(prefix)
	if suffixLen <= 0 {
		return raw
	}

	h := sha256.Sum256([]byte(raw))
	hashHex := hex.EncodeToString(h[:])

	var b strings.Builder
	b.WriteString(prefix)
	for b.Len() < len(raw) {
		needed := len(raw) - b.Len()
		if needed <= len(hashHex) {
			b.WriteString(hashHex[:needed])
		} else {
			b.WriteString(hashHex)
		}
	}
	return b.String()
}

// SubstituteJWT replaces a JWT with a synthetic valid JWT preserving structure.
func SubstituteJWT(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return raw
	}

	// Synthetic header ({"alg":"HS256","typ":"JWT"})
	synthHeader := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	// Synthetic payload ({"sub":"redacted_user","iat":1516239022})
	synthPayload := "eyJzdWIiOiJyZWRhY3RlZF91c2VyIiwiaWF0IjoxNTE2MjM5MDIyfQ"

	// Preserve signature length with deterministic characters
	sigLen := len(parts[2])
	if sigLen == 0 {
		sigLen = 43
	}
	h := sha256.Sum256([]byte(raw))
	hashHex := hex.EncodeToString(h[:])
	var synthSig strings.Builder
	for synthSig.Len() < sigLen {
		needed := sigLen - synthSig.Len()
		if needed <= len(hashHex) {
			synthSig.WriteString(hashHex[:needed])
		} else {
			synthSig.WriteString(hashHex)
		}
	}

	return synthHeader + "." + synthPayload + "." + synthSig.String()
}

// SubstitutePEM replaces the inner content of a PEM block with dummy base64 characters.
func SubstitutePEM(raw string) string {
	lines := strings.Split(raw, "\n")
	if len(lines) < 2 {
		return raw
	}

	var result []string
	header := lines[0]
	footer := lines[len(lines)-1]
	for footer == "" && len(lines) > 2 {
		lines = lines[:len(lines)-1]
		footer = lines[len(lines)-1]
	}

	result = append(result, header)
	dummyLine := "MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7V3D0fakeDummy01"
	for i := 1; i < len(lines)-1; i++ {
		origLine := strings.TrimSpace(lines[i])
		if len(origLine) == 0 {
			continue
		}
		if len(origLine) <= len(dummyLine) {
			result = append(result, dummyLine[:len(origLine)])
		} else {
			result = append(result, dummyLine)
		}
	}
	result = append(result, footer)
	return strings.Join(result, "\n")
}

// SubstituteCookieHeader redacts cookie values while preserving attribute structure.
func SubstituteCookieHeader(raw string) string {
	attributes := map[string]bool{
		"path":     true,
		"domain":   true,
		"expires":  true,
		"max-age":  true,
		"secure":   true,
		"httponly": true,
		"samesite": true,
	}

	parts := strings.Split(raw, ";")
	var newParts []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		subparts := strings.SplitN(trimmed, "=", 2)
		key := strings.TrimSpace(subparts[0])
		lowerKey := strings.ToLower(key)

		if attributes[lowerKey] || len(subparts) == 1 {
			newParts = append(newParts, trimmed)
			continue
		}

		val := subparts[1]
		synthVal := SubstituteToken("tok_"+val, "tok_")
		if len(synthVal) > 4 {
			synthVal = synthVal[4:]
		}
		newParts = append(newParts, key+"="+synthVal)
	}

	return strings.Join(newParts, "; ")
}
