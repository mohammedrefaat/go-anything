// Package redactor enforces fail-closed in-memory secret detection and
// format-preserving synthetic substitution before data reaches disk.
package redactor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

var (
	// ErrUnparseablePayload is returned when a payload declared as structured (e.g. JSON)
	// cannot be parsed, triggering fail-closed protection.
	ErrUnparseablePayload = errors.New("redactor: unparseable payload fails closed")

	// ErrHighConfidenceSecret is returned when an unexpected high-confidence secret is detected
	// in an unredactable context under fail-closed security.
	ErrHighConfidenceSecret = errors.New("redactor: unexpected high-confidence secret detected")
)

// Options configures the Redactor behavior.
type Options struct {
	// UnsafeAllowUnredacted bypasses fail-closed dropping on unparseable payloads.
	// Default is false (fail-closed security per ADR / Execution Rule 3).
	UnsafeAllowUnredacted bool
}

// Redactor performs in-memory secret redaction, format-preserving substitution,
// and unreplayable field marking.
type Redactor struct {
	opts Options
}

// New returns a new Redactor configured with the provided options.
func New(opts ...Options) *Redactor {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	return &Redactor{opts: o}
}

// RedactString processes arbitrary text, identifying and substituting known secret patterns.
func (r *Redactor) RedactString(s string) (string, []string, error) {
	if s == "" {
		return "", nil, nil
	}

	var unreplayable []string

	// 1. PEM Blocks
	s = regexPEMBlock.ReplaceAllStringFunc(s, func(m string) string {
		return SubstitutePEM(m)
	})

	// 2. JWTs
	s = regexJWT.ReplaceAllStringFunc(s, func(m string) string {
		return SubstituteJWT(m)
	})

	// 3. GitHub Tokens (ghp_, gho_, ghs_, ghr_)
	s = regexGitHubToken.ReplaceAllStringFunc(s, func(m string) string {
		prefix := m[:4]
		return SubstituteToken(m, prefix)
	})

	// 4. AWS Access Key IDs
	s = regexAWSAccessKey.ReplaceAllStringFunc(s, func(m string) string {
		return SubstituteToken(m, "AKIA")
	})

	// 5. Stripe Keys (sk_live_, sk_test_, pk_live_, pk_test_)
	s = regexStripeKey.ReplaceAllStringFunc(s, func(m string) string {
		prefixEnd := strings.Index(m, "_")
		if prefixEnd != -1 && prefixEnd+5 < len(m) {
			prefix := m[:prefixEnd+6] // e.g., "sk_live_"
			return SubstituteToken(m, prefix)
		}
		return SubstituteToken(m, "sk_")
	})

	// 5b. Anthropic / LLM Keys (sk-ant-..., sk-proj-...)
	s = regexAnthropicKey.ReplaceAllStringFunc(s, func(m string) string {
		return SubstituteToken(m, "sk-")
	})

	// 6. Slack Tokens (xoxb-, xoxp-, etc.)
	s = regexSlackToken.ReplaceAllStringFunc(s, func(m string) string {
		prefixEnd := strings.Index(m, "-")
		if prefixEnd != -1 {
			return SubstituteToken(m, m[:prefixEnd+1])
		}
		return SubstituteToken(m, "xox-")
	})

	// 7. Bearer Tokens
	s = regexBearerToken.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.SplitN(m, " ", 2)
		if len(parts) == 2 {
			return parts[0] + " " + SubstituteToken("tok_"+parts[1], "tok_")[4:]
		}
		return m
	})

	// 8. Basic Auth
	s = regexBasicAuth.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.SplitN(m, " ", 2)
		if len(parts) == 2 {
			return parts[0] + " " + SubstituteToken("bas_"+parts[1], "bas_")[4:]
		}
		return m
	})

	// 9. Credit Cards with Luhn validation
	s = regexCardCandidate.ReplaceAllStringFunc(s, func(m string) string {
		if IsValidLuhn(m) {
			return SubstituteCreditCard(m)
		}
		return m
	})

	// 10. Email Addresses
	s = regexEmail.ReplaceAllStringFunc(s, func(m string) string {
		return SubstituteEmail(m)
	})

	return s, unreplayable, nil
}

// RedactHeaders redacts HTTP headers, substituting credentials and marking signatures as unreplayable.
func (r *Redactor) RedactHeaders(headers map[string]string) (map[string]string, []string, error) {
	if headers == nil {
		return nil, nil, nil
	}

	result := make(map[string]string, len(headers))
	var unreplayable []string

	for k, v := range headers {
		if IsSignatureOrMACField(k) {
			unreplayable = append(unreplayable, k)
			result[k] = "[unreplayable]"
			continue
		}

		lower := strings.ToLower(k)
		switch lower {
		case "authorization", "proxy-authorization":
			redacted, _, _ := r.RedactString(v)
			result[k] = redacted

		case "cookie", "set-cookie":
			result[k] = SubstituteCookieHeader(v)

		default:
			if IsSecretFieldName(k) {
				result[k] = SubstituteToken("sec_"+v, "sec_")[4:]
			} else {
				redacted, unrep, _ := r.RedactString(v)
				result[k] = redacted
				unreplayable = append(unreplayable, unrep...)
			}
		}
	}

	return result, unreplayable, nil
}

// RedactValue recursively walks maps, slices, and primitive values, applying redaction.
func (r *Redactor) RedactValue(val any, path string) (any, []string, error) {
	var unreplayable []string

	switch v := val.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, item := range v {
			fieldPath := k
			if path != "" {
				fieldPath = path + "." + k
			}

			if IsSignatureOrMACField(k) {
				unreplayable = append(unreplayable, fieldPath)
				result[k] = "[unreplayable]"
				continue
			}

			if strVal, ok := item.(string); ok {
				trimmed := strings.TrimSpace(strVal)
				embeddedJSON := strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
				secret := IsSecretFieldName(k)
				if secret && embeddedJSON {
					result[k] = SubstituteToken("sec_"+strVal, "sec_")[4:]
					continue
				}
				if embeddedJSON {
					var parsed, extra any
					dec := json.NewDecoder(strings.NewReader(trimmed))
					dec.UseNumber()
					if dec.Decode(&parsed) == nil && dec.Decode(&extra) == io.EOF {
						redactedParsed, parsedUnrep, err := r.RedactValue(parsed, fieldPath)
						if err != nil {
							return nil, nil, err
						}
						outBytes, err := json.Marshal(redactedParsed)
						if err != nil {
							return nil, nil, err
						}
						result[k] = string(outBytes)
						unreplayable = append(unreplayable, parsedUnrep...)
						continue
					}
					if !r.opts.UnsafeAllowUnredacted {
						return nil, nil, fmt.Errorf("%w: malformed embedded JSON at %s", ErrUnparseablePayload, fieldPath)
					}
				}

				redactedChild, childUnrep, err := r.RedactString(strVal)
				if err != nil {
					return nil, nil, err
				}
				if redactedChild != strVal {
					result[k] = redactedChild
					unreplayable = append(unreplayable, childUnrep...)
					continue
				}

				if secret {
					result[k] = SubstituteToken("sec_"+strVal, "sec_")[4:]
					continue
				}

				result[k] = redactedChild
				unreplayable = append(unreplayable, childUnrep...)
				continue
			}

			// Recursively redact non-string fields (numbers, booleans, nested maps, arrays)
			redactedChild, childUnrep, err := r.RedactValue(item, fieldPath)
			if err != nil {
				return nil, nil, err
			}
			result[k] = redactedChild
			unreplayable = append(unreplayable, childUnrep...)
		}
		return result, unreplayable, nil

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			elemPath := fmt.Sprintf("%s[%d]", path, i)
			redactedElem, elemUnrep, err := r.RedactValue(item, elemPath)
			if err != nil {
				return nil, nil, err
			}
			result[i] = redactedElem
			unreplayable = append(unreplayable, elemUnrep...)
		}
		return result, unreplayable, nil

	case string:
		redactedStr, strUnrep, err := r.RedactString(v)
		if err != nil {
			return nil, nil, err
		}
		unreplayable = append(unreplayable, strUnrep...)
		return redactedStr, unreplayable, nil

	default:
		return val, nil, nil
	}
}

// RedactJSON redacts a raw JSON payload, failing closed if JSON is malformed unless UnsafeAllowUnredacted is set.
func (r *Redactor) RedactJSON(data []byte) ([]byte, []string, error) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		if !r.opts.UnsafeAllowUnredacted {
			return nil, nil, fmt.Errorf("%w: invalid JSON syntax: %v", ErrUnparseablePayload, err)
		}
		redactedStr, unrep, _ := r.RedactString(string(data))
		return []byte(redactedStr), unrep, nil
	}

	redactedVal, unreplayable, err := r.RedactValue(parsed, "")
	if err != nil {
		return nil, nil, err
	}

	out, err := json.Marshal(redactedVal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to re-encode redacted JSON: %w", err)
	}

	return out, unreplayable, nil
}

// RedactCase redacts all input and expected output payloads in an adapterkit.Case,
// recording unreplayable signature/MAC fields in Case.UnreplayableFields.
func (r *Redactor) RedactCase(c *adapterkit.Case) (*adapterkit.Case, error) {
	if c == nil {
		return nil, nil
	}

	unreplayableSet := make(map[string]bool)
	for _, uf := range c.UnreplayableFields {
		unreplayableSet[uf] = true
	}

	// 1. Redact Input Headers
	if c.Input.Headers != nil {
		redactedHeaders, unrep, err := r.RedactHeaders(c.Input.Headers)
		if err != nil {
			return nil, err
		}
		c.Input.Headers = redactedHeaders
		for _, u := range unrep {
			unreplayableSet["input.headers."+u] = true
		}
	}

	// 2. Redact Input Body
	if c.Input.Body != nil {
		redactedBody, unrep, err := r.redactBody(c.Input.Body, "input.body")
		if err != nil {
			return nil, err
		}
		c.Input.Body = redactedBody
		for _, u := range unrep {
			unreplayableSet[u] = true
		}
	}

	// 3. Redact Input Env
	if c.Input.Env != nil {
		for k, v := range c.Input.Env {
			if IsSecretFieldName(k) {
				c.Input.Env[k] = SubstituteToken("sec_"+v, "sec_")[4:]
			} else {
				redacted, _, _ := r.RedactString(v)
				c.Input.Env[k] = redacted
			}
		}
	}

	// 4. Redact Expected Output Headers
	if c.ExpectedOutput.Headers != nil {
		redactedHeaders, unrep, err := r.RedactHeaders(c.ExpectedOutput.Headers)
		if err != nil {
			return nil, err
		}
		c.ExpectedOutput.Headers = redactedHeaders
		for _, u := range unrep {
			unreplayableSet["expected_output.headers."+u] = true
		}
	}

	// 5. Redact Expected Output Body
	if c.ExpectedOutput.Body != nil {
		redactedBody, unrep, err := r.redactBody(c.ExpectedOutput.Body, "expected_output.body")
		if err != nil {
			return nil, err
		}
		c.ExpectedOutput.Body = redactedBody
		for _, u := range unrep {
			unreplayableSet[u] = true
		}
	}

	// 6. Redact Output Stdout & Stderr
	if c.ExpectedOutput.Stdout != "" {
		redactedStdout, _, _ := r.RedactString(c.ExpectedOutput.Stdout)
		c.ExpectedOutput.Stdout = redactedStdout
	}
	if c.ExpectedOutput.Stderr != "" {
		redactedStderr, _, _ := r.RedactString(c.ExpectedOutput.Stderr)
		c.ExpectedOutput.Stderr = redactedStderr
	}

	// 7. Redact Input Path (e.g. secret tokens in query string or path)
	if c.Input.Path != "" {
		redactedPath, _, _ := r.RedactString(c.Input.Path)
		c.Input.Path = redactedPath
	}

	// 8. Redact Input Argv
	if len(c.Input.Argv) > 0 {
		for i, arg := range c.Input.Argv {
			redactedArg, _, _ := r.RedactString(arg)
			c.Input.Argv[i] = redactedArg
		}
	}

	// 9. Redact Metadata values
	if c.Metadata != nil {
		redactedMeta, unrep, err := r.RedactValue(c.Metadata, "metadata")
		if err != nil {
			return nil, err
		}
		c.Metadata = redactedMeta.(map[string]any)
		for _, u := range unrep {
			unreplayableSet[u] = true
		}
	}

	// Update Case.UnreplayableFields
	var unreplayableList []string
	for k := range unreplayableSet {
		unreplayableList = append(unreplayableList, k)
	}
	sort.Strings(unreplayableList)
	c.UnreplayableFields = unreplayableList

	return c, nil
}

func (r *Redactor) redactBody(body any, path string) (any, []string, error) {
	switch b := body.(type) {
	case string:
		trimmed := strings.TrimSpace(b)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var parsed any
			if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
				if !r.opts.UnsafeAllowUnredacted {
					return nil, nil, fmt.Errorf("%w: failed to parse JSON body at %s: %v", ErrUnparseablePayload, path, err)
				}
				redactedStr, unrep, _ := r.RedactString(b)
				return redactedStr, unrep, nil
			}
			redactedParsed, unrep, err := r.RedactValue(parsed, path)
			if err != nil {
				return nil, nil, err
			}
			outBytes, err := json.Marshal(redactedParsed)
			if err != nil {
				return nil, nil, err
			}
			return string(outBytes), unrep, nil
		}
		redactedStr, unrep, _ := r.RedactString(b)
		return redactedStr, unrep, nil

	default:
		return r.RedactValue(body, path)
	}
}
