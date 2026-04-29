package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseAccountLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		email    string
		password string
	}{
		{name: "dash separated", line: " user@example.com ---- secret ", email: "user@example.com", password: "secret"},
		{name: "comma separated", line: "user@example.com,secret", email: "user@example.com", password: "secret"},
		{name: "tab separated", line: "user@example.com\tsecret", email: "user@example.com", password: "secret"},
		{name: "json", line: `{"email":" user@example.com ","password":"secret"}`, email: "user@example.com", password: "secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc, err := parseAccountLine(tt.line)
			if err != nil {
				t.Fatalf("parseAccountLine() error = %v", err)
			}
			if acc.Email != tt.email || acc.Password != tt.password {
				t.Fatalf("parseAccountLine() = %#v, want email=%q password=%q", acc, tt.email, tt.password)
			}
		})
	}
}

func TestParseAccountLineRejectsInvalidInput(t *testing.T) {
	for _, line := range []string{"user@example.com", "----secret", `{"email":"user@example.com"}`} {
		if _, err := parseAccountLine(line); err == nil {
			t.Fatalf("parseAccountLine(%q) expected error", line)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := map[string]string{
		"":               "k9ray.com",
		" @example.com ": "example.com",
		"example.com":    "example.com",
	}
	for in, want := range tests {
		if got := normalizeDomain(in); got != want {
			t.Fatalf("normalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateGeneratedAccounts(t *testing.T) {
	accounts, err := createGeneratedAccounts(2, "@example.com", 12, false, nil)
	if err != nil {
		t.Fatalf("createGeneratedAccounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
	seen := map[string]bool{}
	for _, acc := range accounts {
		if !strings.HasSuffix(acc.Email, "@example.com") {
			t.Fatalf("email %q does not use normalized domain", acc.Email)
		}
		if len(acc.Password) != 12 {
			t.Fatalf("password length = %d, want 12", len(acc.Password))
		}
		if seen[acc.Email] {
			t.Fatalf("duplicate generated email %q", acc.Email)
		}
		seen[acc.Email] = true
	}
}

func TestPromptGenerateOptionsRejectsShortPassword(t *testing.T) {
	stdin := bufio.NewReader(strings.NewReader("y\n1\nexample.com\n7\n"))
	_, _, _, err := promptGenerateOptions("example.com", 14, stdin)
	if err == nil {
		t.Fatal("promptGenerateOptions() expected error")
	}
}
