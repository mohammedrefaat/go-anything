package redactor

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Kind identifies the category of secret detected.
type Kind string

const (
	KindPEM               Kind = "pem"
	KindJWT               Kind = "jwt"
	KindGitHubToken       Kind = "github_token"
	KindAWSAccessKey      Kind = "aws_access_key"
	KindAWSSecretKey      Kind = "aws_secret_key"
	KindGoogleAPIKey      Kind = "google_api_key"
	KindSendGridKey       Kind = "sendgrid_key"
	KindNPMToken          Kind = "npm_token"
	KindTwilioKey         Kind = "twilio_key"
	KindStripeKey         Kind = "stripe_key"
	KindAnthropicKey      Kind = "anthropic_key"
	KindSlackToken        Kind = "slack_token"
	KindURLCredential     Kind = "url_credential"
	KindBearerToken       Kind = "bearer_token"
	KindBasicAuth         Kind = "basic_auth"
	KindCreditCard        Kind = "credit_card"
	KindGenericAssignment Kind = "generic_assignment"
	KindEmail             Kind = "email"
)

// Finding describes a detected secret span within an input string.
// It records byte offsets rather than the raw secret value.
type Finding struct {
	Kind       Kind
	Start      int
	End        int
	Confidence string
}

const awsSecretWindow = 160

var (
	// regexJWT matches JSON Web Tokens (3 base64url parts starting with eyJ).
	regexJWT = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)

	// regexGitHubToken matches GitHub personal access and oauth tokens.
	regexGitHubToken = regexp.MustCompile(`\b(?:ghp|gho|ghs|ghr)_[A-Za-z0-9]{36}\b`)

	// regexAWSAccessKey matches AWS Access Key IDs.
	regexAWSAccessKey = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)

	// regexAWSSecretAnchor matches nearby context indicating an AWS secret access key.
	regexAWSSecretAnchor = regexp.MustCompile(`(?i)(?:\bAKIA[0-9A-Z]{16}\b|\baws[_-]?(?:secret[_-]?(?:access[_-]?)?key)\b|\bsecret_access_key\b)`)

	// regexAWSSecretCandidate matches 40+ char base64-ish runs after a delimiter or start of string; callers check exact length == 40.
	regexAWSSecretCandidate = regexp.MustCompile(`(?:^|[=:\s"'` + "`" + `])([A-Za-z0-9/+=]{40,})`)

	// regexGoogleAPIKey matches Google API keys.
	regexGoogleAPIKey = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)

	// regexSendGridKey matches SendGrid API keys.
	regexSendGridKey = regexp.MustCompile(`\bSG\.[\w-]{22}\.[\w-]{43}\b`)

	// regexNPMToken matches npm access tokens.
	regexNPMToken = regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)

	// regexTwilioKey matches Twilio API keys.
	regexTwilioKey = regexp.MustCompile(`\bSK[0-9a-f]{32}\b`)

	// regexStripeKey matches Stripe secret and publishable keys.
	regexStripeKey = regexp.MustCompile(`\b(?:sk|pk)_(?:live|test)_[0-9a-zA-Z]{24,}\b`)

	// regexAnthropicKey matches Anthropic / OpenAI style secret keys (e.g. sk-ant-..., sk-proj-..., or 32+ char legacy sk-...).
	regexAnthropicKey = regexp.MustCompile(`\bsk-(?:(?:ant|proj|svcacct|admin|live|test)-[A-Za-z0-9_-]{16,}|[A-Za-z0-9]{32,})\b`)

	// regexSlackToken matches Slack user and bot tokens.
	regexSlackToken = regexp.MustCompile(`\bxox[baprs]-[0-9a-zA-Z-]{10,}\b`)

	// regexURLCredential matches URLs with embedded user:password@ credentials, capturing the password in group 2.
	regexURLCredential = regexp.MustCompile(`\b([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^/\s@]+)(@)`)

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

	// regexGenericAssignment matches key=value or key:value with a quoted secret >= 8 chars.
	regexGenericAssignment = regexp.MustCompile(`(?i)\b[a-z0-9_.-]*(?:pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|client[_-]?secret)[a-z0-9_.-]*["']?\s*[:=]\s*(?:"([^"\r\n]{8,})"|'([^'\r\n]{8,})'|` + "`([^`\\r\\n]{8,})`" + `)`)
)

// Detect scans s for secret patterns without mutating it and returns non-overlapping
// findings sorted deterministically by start offset.
func Detect(s string) []Finding {
	if s == "" {
		return nil
	}

	confidenceRank := func(c string) int {
		switch c {
		case "high":
			return 3
		case "medium":
			return 2
		default:
			return 1
		}
	}

	var accepted []Finding
	add := func(kind Kind, start, end int, confidence string) {
		if start >= end {
			return
		}
		var filtered []Finding
		replaced := false
		for _, existing := range accepted {
			if start < existing.End && existing.Start < end {
				if start <= existing.Start && existing.End <= end && confidenceRank(confidence) > confidenceRank(existing.Confidence) {
					// Strictly higher-confidence enclosing finding replaces narrower inner finding(s).
					replaced = true
					continue
				}
				return
			}
			filtered = append(filtered, existing)
		}
		if replaced {
			accepted = filtered
		}
		accepted = append(accepted, Finding{
			Kind:       kind,
			Start:      start,
			End:        end,
			Confidence: confidence,
		})
	}

	addMatches := func(re *regexp.Regexp, kind Kind, confidence string) {
		for _, m := range re.FindAllStringIndex(s, -1) {
			add(kind, m[0], m[1], confidence)
		}
	}

	addGroup := func(re *regexp.Regexp, group int, kind Kind, confidence string) {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			if len(m) > 2*group+1 && m[2*group] >= 0 {
				add(kind, m[2*group], m[2*group+1], confidence)
			}
		}
	}

	addMatches(regexPEMBlock, KindPEM, "high")
	addMatches(regexJWT, KindJWT, "high")
	addMatches(regexGitHubToken, KindGitHubToken, "high")
	addMatches(regexAWSAccessKey, KindAWSAccessKey, "high")

	if anchors := regexAWSSecretAnchor.FindAllStringIndex(s, -1); len(anchors) > 0 {
		for _, m := range regexAWSSecretCandidate.FindAllStringSubmatchIndex(s, -1) {
			if len(m) < 4 || m[2] < 0 {
				continue
			}
			start, end := m[2], m[3]
			if end-start != 40 {
				continue
			}
			for _, a := range anchors {
				if start <= a[1]+awsSecretWindow && end >= a[0]-awsSecretWindow {
					add(KindAWSSecretKey, start, end, "high")
					break
				}
			}
		}
	}

	addMatches(regexGoogleAPIKey, KindGoogleAPIKey, "high")
	addMatches(regexSendGridKey, KindSendGridKey, "high")
	addMatches(regexNPMToken, KindNPMToken, "high")
	addMatches(regexTwilioKey, KindTwilioKey, "high")
	addMatches(regexStripeKey, KindStripeKey, "high")
	addMatches(regexAnthropicKey, KindAnthropicKey, "high")
	addMatches(regexSlackToken, KindSlackToken, "high")
	addGroup(regexURLCredential, 2, KindURLCredential, "high")
	addGroup(regexBearerToken, 1, KindBearerToken, "high")
	addGroup(regexBasicAuth, 1, KindBasicAuth, "high")

	for _, m := range regexCardCandidate.FindAllStringIndex(s, -1) {
		if IsValidLuhn(s[m[0]:m[1]]) {
			add(KindCreditCard, m[0], m[1], "high")
		}
	}

	for _, m := range regexGenericAssignment.FindAllStringSubmatchIndex(s, -1) {
		for g := 1; g <= 3; g++ {
			if len(m) > 2*g+1 && m[2*g] >= 0 {
				val := s[m[2*g]:m[2*g+1]]
				if !isPlaceholderSecret(val) {
					add(KindGenericAssignment, m[2*g], m[2*g+1], "medium")
				}
				break
			}
		}
	}

	addMatches(regexEmail, KindEmail, "medium")

	sort.Slice(accepted, func(i, j int) bool {
		if accepted[i].Start != accepted[j].Start {
			return accepted[i].Start < accepted[j].Start
		}
		return accepted[i].End < accepted[j].End
	})

	return accepted
}

func isPlaceholderSecret(val string) bool {
	lower := strings.ToLower(strings.TrimSpace(val))
	if lower == "" {
		return true
	}
	switch lower {
	case "changeme", "change_me", "change-me", "example", "placeholder",
		"your_secret_here", "your-secret-here", "replace_me", "todo", "none", "null", "dummy":
		return true
	}
	if strings.Trim(lower, "x*") == "" {
		return true
	}
	if strings.HasPrefix(lower, "changeme") ||
		strings.HasPrefix(lower, "change_me") ||
		strings.HasPrefix(lower, "change-me") ||
		strings.HasPrefix(lower, "example_") ||
		strings.HasPrefix(lower, "example-") ||
		strings.HasPrefix(lower, "os.getenv") {
		return true
	}
	if strings.HasPrefix(lower, "<") && strings.HasSuffix(lower, ">") {
		return true
	}
	if strings.HasPrefix(lower, "${") && strings.HasSuffix(lower, "}") {
		return true
	}
	return false
}

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
