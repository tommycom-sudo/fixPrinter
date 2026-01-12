package monitor

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// ParseCURLCommand parses a curl command string and returns a ParsedRequest
func ParseCURLCommand(curlCmd string) (*ParsedRequest, error) {
	// Remove continuation characters and normalize line breaks
	curlCmd = strings.ReplaceAll(curlCmd, "\\\n", " ")
	curlCmd = strings.ReplaceAll(curlCmd, "\\\r\n", " ")

	// Tokenize while respecting quoted strings
	tokens, err := tokenizePreservingQuotes(curlCmd)
	if err != nil {
		return nil, err
	}

	req := &ParsedRequest{
		Method:  "GET",
		Headers: make(map[string]string),
	}

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]

		switch {
		case token == "-X" || token == "--request":
			if i+1 < len(tokens) {
				req.Method = strings.ToUpper(tokens[i+1])
				i++
			}

		case token == "-H" || token == "--header":
			if i+1 < len(tokens) {
				headerParts := strings.SplitN(tokens[i+1], ":", 2)
				if len(headerParts) == 2 {
					key := strings.TrimSpace(headerParts[0])
					value := strings.TrimSpace(headerParts[1])
					// Remove quotes from header values
					value = strings.Trim(value, `"'`)
					req.Headers[key] = value
				}
				i++
			}

		case token == "-b" || token == "--cookie":
			if i+1 < len(tokens) {
				// Parse cookie string and set as Cookie header
				cookieStr := strings.Trim(tokens[i+1], `"'`)
				if req.Headers["Cookie"] != "" {
					req.Headers["Cookie"] += "; " + cookieStr
				} else {
					req.Headers["Cookie"] = cookieStr
				}
				i++
			}

		case token == "-d" || token == "--data" || token == "--data-raw" || token == "--data-binary":
			if i+1 < len(tokens) {
				req.Method = "POST"
				req.Body = strings.Trim(tokens[i+1], `"'`)
				// Ensure Content-Type is set
				if req.Headers["Content-Type"] == "" {
					req.Headers["Content-Type"] = "application/json"
				}
				i++
			}

		case token == "--data-urlencode":
			if i+1 < len(tokens) {
				req.Method = "POST"
				data := tokens[i+1]
				// URL encode the data
				if req.Body == "" {
					req.Body = url.QueryEscape(data)
				} else {
					req.Body += "&" + url.QueryEscape(data)
				}
				if req.Headers["Content-Type"] == "" {
					req.Headers["Content-Type"] = "application/x-www-form-urlencoded"
				}
				i++
			}

		case token == "-F" || token == "--form":
			if i+1 < len(tokens) {
				req.Method = "POST"
				if req.Headers["Content-Type"] == "" {
					req.Headers["Content-Type"] = "multipart/form-data"
				}
				// Form data handling
				formData := tokens[i+1]
				if req.Body == "" {
					req.Body = formData
				} else {
					req.Body += "&" + formData
				}
				i++
			}

		case strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--"):
			// Skip short flags we don't handle
			if len(token) == 2 && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				i++
			}

		case strings.HasPrefix(token, "--"):
			// Skip long flags we don't handle
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				i++
			}

		default:
			// Check if this is a URL
			if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
				req.URL = token
			} else if strings.HasPrefix(token, "'") || strings.HasPrefix(token, `"`) {
				unquoted := strings.Trim(token, `"'`)
				if strings.HasPrefix(unquoted, "http://") || strings.HasPrefix(unquoted, "https://") {
					req.URL = unquoted
				}
			}
		}
	}

	// Remove empty X-Client-Ip and X-Client-Mac headers (curl weirdness)
	delete(req.Headers, "X-Client-Ip")
	delete(req.Headers, "X-Client-Mac")

	if req.URL == "" {
		return nil, errors.New("no URL found in curl command")
	}

	return req, nil
}

// tokenizePreservingQuotes tokenizes a string while preserving quoted content
func tokenizePreservingQuotes(s string) ([]string, error) {
	var tokens []string
	var currentToken strings.Builder
	var inQuotes bool
	var quoteChar rune
	var escapeNext bool

	for _, r := range s {
		if escapeNext {
			currentToken.WriteRune(r)
			escapeNext = false
			continue
		}

		switch r {
		case '\\':
			escapeNext = true
		case '\'', '"':
			if !inQuotes {
				inQuotes = true
				quoteChar = r
			} else if r == quoteChar {
				inQuotes = false
				quoteChar = 0
			} else {
				currentToken.WriteRune(r)
			}
		default:
			if unicode.IsSpace(r) && !inQuotes {
				if currentToken.Len() > 0 {
					tokens = append(tokens, currentToken.String())
					currentToken.Reset()
				}
			} else {
				currentToken.WriteRune(r)
			}
		}
	}

	if currentToken.Len() > 0 {
		tokens = append(tokens, currentToken.String())
	}

	if inQuotes {
		return nil, errors.New("unclosed quote in command")
	}

	return tokens, nil
}

// ToHTTPRequest converts a ParsedRequest to an http.Request
func (p *ParsedRequest) ToHTTPRequest() (*http.Request, error) {
	req, err := http.NewRequest(p.Method, p.URL, strings.NewReader(p.Body))
	if err != nil {
		return nil, err
	}

	for key, value := range p.Headers {
		req.Header.Set(key, value)
	}

	return req, nil
}

// Validate validates the parsed request
func (p *ParsedRequest) Validate() error {
	if p.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if _, err := url.Parse(p.URL); err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	return nil
}
