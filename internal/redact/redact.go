package redact

import (
	"net/url"
	"strings"
	"unicode"
)

var sensitiveQueryKeys = map[string]struct{}{
	"access_token": {},
	"apikey":       {},
	"api_key":      {},
	"auth":         {},
	"bearer":       {},
	"jwt":          {},
	"key":          {},
	"password":     {},
	"secret":       {},
	"token":        {},
}

func URL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return redactLoose(raw)
	}
	if u.User != nil {
		u.User = url.User("redacted")
	}
	q := u.Query()
	for key := range q {
		if isSensitiveKey(key) {
			q.Set(key, "redacted")
		}
	}
	u.RawQuery = q.Encode()
	u.Path = redactPath(u.Path)
	u.RawPath = ""
	return u.String()
}

func String(s string, rawURLs ...string) string {
	out := s
	for _, raw := range rawURLs {
		if raw == "" {
			continue
		}
		out = strings.ReplaceAll(out, raw, URL(raw))
	}
	return redactLoose(out)
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	if _, ok := sensitiveQueryKeys[key]; ok {
		return true
	}
	return strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password")
}

func redactPath(path string) string {
	if path == "" || path == "/" {
		return path
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		prev := ""
		if i > 0 {
			prev = strings.ToLower(parts[i-1])
		}
		if looksLikeToken(part) || (isTokenPrefix(prev) && len(part) > 6) {
			parts[i] = "redacted"
		}
	}
	return strings.Join(parts, "/")
}

func isTokenPrefix(part string) bool {
	switch part {
	case "key", "token", "v2", "api-key", "apikey", "secret":
		return true
	default:
		return false
	}
}

func looksLikeToken(part string) bool {
	if len(part) < 20 {
		return false
	}
	var letters, digits int
	for _, r := range part {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return letters > 0 && digits > 0
}

func redactLoose(s string) string {
	for _, marker := range []string{"access_token=", "api_key=", "apikey=", "token=", "secret=", "password="} {
		idx := strings.Index(strings.ToLower(s), marker)
		for idx >= 0 {
			start := idx + len(marker)
			end := start
			for end < len(s) && s[end] != '&' && !unicode.IsSpace(rune(s[end])) {
				end++
			}
			s = s[:start] + "redacted" + s[end:]
			idx = strings.Index(strings.ToLower(s[start:]), marker)
			if idx >= 0 {
				idx += start
			}
		}
	}
	return s
}
