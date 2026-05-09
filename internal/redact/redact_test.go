package redact

import (
	"strings"
	"testing"
)

func TestURLRedactsCredentialsQueryAndPathTokens(t *testing.T) {
	raw := "https://user:pass@example.com/v2/abc1234567890abcdefghi?token=secret&foo=bar"
	got := URL(raw)
	for _, leaked := range []string{"user", "pass", "abc1234567890abcdefghi", "secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("URL() leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "foo=bar") {
		t.Fatalf("URL() should preserve non-sensitive query values, got %q", got)
	}
}

func TestStringRedactsKnownURL(t *testing.T) {
	raw := "http://example.com?api_key=shh"
	got := String("Post "+raw+": failed", raw)
	if strings.Contains(got, "shh") {
		t.Fatalf("String() leaked secret: %q", got)
	}
	if !strings.Contains(got, "redacted") {
		t.Fatalf("String() did not mark redaction: %q", got)
	}
}
