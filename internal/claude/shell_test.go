package claude

import "testing"

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"":             "''",
		"/tmp/a.stop":  "'/tmp/a.stop'",
		"/tmp/a b":     "'/tmp/a b'",
		"/tmp/it's ok": `'/tmp/it'\''s ok'`,
	}

	for input, want := range tests {
		if got := shellQuote(input); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}
