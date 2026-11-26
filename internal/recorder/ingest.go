package recorder

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// SplitTokens splits a command string into argument tokens respecting single and double quotes.
func SplitTokens(commandLine string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i, r := range commandLine {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && !inSingle {
			escaped = true
			continue
		}

		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if unicode.IsSpace(r) && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
		_ = i
	}

	if inSingle || inDouble {
		return nil, errors.New("unmatched quotation mark in command line")
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

// ParseCLI parses a CLI command string into an adapterkit.CaseInput.
func ParseCLI(cliCommand string) (*adapterkit.CaseInput, error) {
	tokens, err := SplitTokens(cliCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CLI command: %w", err)
	}

	if len(tokens) == 0 {
		return nil, errors.New("empty CLI command")
	}

	return &adapterkit.CaseInput{
		Argv: tokens,
	}, nil
}

// ParseCurl parses a curl command string into an adapterkit.CaseInput.
func ParseCurl(curlCommand string) (*adapterkit.CaseInput, error) {
	tokens, err := SplitTokens(curlCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to parse curl command: %w", err)
	}

	if len(tokens) == 0 {
		return nil, errors.New("empty curl command")
	}

	// Skip leading "curl" token if present
	if strings.ToLower(tokens[0]) == "curl" {
		tokens = tokens[1:]
	}

	input := &adapterkit.CaseInput{
		Method:  "GET",
		Headers: make(map[string]string),
	}

	hasBody := false
	var rawBody string

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]

		switch t {
		case "-X", "--request":
			if i+1 < len(tokens) {
				input.Method = strings.ToUpper(tokens[i+1])
				i++
			}

		case "-H", "--header":
			if i+1 < len(tokens) {
				headerStr := tokens[i+1]
				parts := strings.SplitN(headerStr, ":", 2)
				if len(parts) == 2 {
					input.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
				i++
			}

		case "-d", "--data", "--data-raw", "--data-binary":
			if i+1 < len(tokens) {
				rawBody = tokens[i+1]
				hasBody = true
				if input.Method == "GET" {
					input.Method = "POST"
				}
				i++
			}

		default:
			// If not a flag, treat as URL / Path
			if !strings.HasPrefix(t, "-") && input.Path == "" {
				u, err := url.Parse(t)
				if err == nil && u.Path != "" {
					input.Path = u.Path
					if u.RawQuery != "" {
						input.Path += "?" + u.RawQuery
					}
				} else {
					input.Path = t
				}
			}
		}
	}

	if hasBody {
		// Attempt to parse body as structured JSON if possible
		var parsedJSON any
		if json.Unmarshal([]byte(rawBody), &parsedJSON) == nil {
			input.Body = parsedJSON
		} else {
			input.Body = rawBody
		}
	}

	return input, nil
}
