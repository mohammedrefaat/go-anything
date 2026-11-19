package redactor

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// regexJWT matches JSON Web Tokens (3 base64url parts starting with eyJ).
	regexJWT = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)

	// regexGitHubToken matches GitHub personal access and oauth tokens.
	regexGitHubToken = regexp.MustCompile(`\b(?:ghp|gho|ghs|ghr)_[A-Za-z0-9]{36}\b`)

	// regexAWSAccessKey matches AWS Access Key IDs.
	regexAWSAccessKey = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)

	// regexStripeKey matches Stripe secret and publishable keys.
	regexStripeKey = regexp.MustCompile(`\b(?:sk|pk)_(?:live|test)_[0-9a-zA-Z]{24,}\b`)

	// regexSlackToken matches Slack user and bot tokens.
	regexSlackToken = regexp.MustCompile(`\bxox[baprs]-[0-9a-zA-Z-]{10,}\b`)

	// regexPEMBlock matches PEM formatted private keys and certificates.
	regexPEMBlock = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----[\s\S]*?-----END [A-Z0-9 ]+-----`)

	// regexEmail matches standard email addresses.
	regexEmail = regexp.MustCompile(`\b[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}\b`)

	// regexBearerToken matches Bearer authentication tokens.
	regexBearerToken = regexp.MustCompile(`(?i)\bBearer\s+([A-Za-z0-9._~+/-]+=*)`)

	// regexBasicAuth matches Basic authentication headers.
	regexBasicAuth = regexp.MustCompile(`(?i)\bBasic\s+([A-Za-z0-9+/=]+)`)

	// regexCardCandidate matches 13 to 19 digit sequences with optional dashes or spaces.
	regexCardCandidate = regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)
)

// IsValidLuhn checks whether a number string satisfies the Luhn algorithm.
func IsValidLuhn(number string) bool {
	digits := make([]int, 0, len(number))
	for _, r := range number {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		} else if r == ' ' || r == '-' {
			continue
		} else {
			return false
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}

	sum := 0
	double := false
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
	return sum%10 == 0
}

// IsSignatureOrMACField returns true if the field or header name represents a cryptographic signature or MAC.
func IsSignatureOrMACField(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "-", "_")

	signatureFields := map[string]bool{
		"signature":           true,
		"sig":                 true,
		"x_signature":         true,
		"x_signature_256":     true,
		"x_signature_sha256":  true,
		"x_hub_signature":     true,
		"x_hub_signature_256": true,
		"hmac":                true,
		"x_hmac":              true,
		"x_hmac_sha256":       true,
		"mac":                 true,
		"digest":              true,
		"stripe_signature":    true,
		"checksum":            true,
		"token_sig":           true,
		"auth_signature":      true,
	}

	if signatureFields[normalized] {
		return true
	}

	for field := range signatureFields {
		if strings.HasSuffix(normalized, "_"+field) || strings.HasPrefix(normalized, field+"_") {
			return true
		}
	}
	return false
}

// IsSecretFieldName returns true if the JSON field or header key indicates a secret.
func IsSecretFieldName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "-", "_")

	secretFields := map[string]bool{
		"authorization":       true,
		"proxy_authorization": true,
		"cookie":              true,
		"set_cookie":          true,
		"password":            true,
		"passwd":              true,
		"secret":              true,
		"secret_key":          true,
		"api_key":             true,
		"apikey":              true,
		"access_token":        true,
		"auth_token":          true,
		"client_secret":       true,
		"private_key":         true,
		"token":               true,
		"refresh_token":       true,
	}

	if secretFields[normalized] {
		return true
	}

	for sf := range secretFields {
		if strings.Contains(normalized, sf) {
			return true
		}
	}
	return false
}

// CleanDigits extracts only numerical digits from a string.
func CleanDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
