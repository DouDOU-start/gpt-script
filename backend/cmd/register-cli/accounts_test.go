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
			acc, ok, err := parseAccountLine(tt.line)
			if err != nil {
				t.Fatalf("parseAccountLine() error = %v", err)
			}
			if !ok {
				t.Fatal("parseAccountLine() skipped account")
			}
			if acc.Email != tt.email || acc.Password != tt.password {
				t.Fatalf("parseAccountLine() = %#v, want email=%q password=%q", acc, tt.email, tt.password)
			}
		})
	}
}

func TestParseAccountLineSkipsFailedRunResult(t *testing.T) {
	_, ok, err := parseAccountLine(`{"task_id":"task_1","email":"user@example.com","password":"secret","success":false}`)
	if err != nil {
		t.Fatalf("parseAccountLine() error = %v", err)
	}
	if ok {
		t.Fatal("parseAccountLine() parsed failed result")
	}
}

func TestParseAccountLineRejectsInvalidInput(t *testing.T) {
	for _, line := range []string{"user@example.com", "----secret", `{"email":"user@example.com"}`} {
		if _, _, err := parseAccountLine(line); err == nil {
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

func TestRunInteractiveWizardCanChooseDBSource(t *testing.T) {
	stdin := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"2",
		"3",
		"banned",
		"25",
		"",
		"",
		"2",
		"",
		"",
		"results/login.jsonl",
		"",
	}, "\n") + "\n"))
	mode := "register"
	accountsPath := ""
	generateCount := 0
	emailDomain := "k9ray.com"
	passwordLength := 14
	backend := ""
	proxyURL := ""
	outputPath := "results/register_results.jsonl"
	screenshotDir := "screenshots"
	workers := 1
	headless := true
	includeSecrets := true
	dbSource := false
	dbStatus := ""
	dbLimit := 0

	accounts, source, err := runInteractiveWizard(stdin, true, &mode, &accountsPath, &generateCount, &emailDomain, &passwordLength, &backend, &proxyURL, &outputPath, &screenshotDir, &workers, &headless, &includeSecrets, &dbSource, &dbStatus, &dbLimit)
	if err != nil {
		t.Fatalf("runInteractiveWizard() error = %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("accounts = %#v, want empty until DB load", accounts)
	}
	if mode != "login" || source != "db" || !dbSource || dbStatus != "banned" || dbLimit != 25 {
		t.Fatalf("mode=%q source=%q dbSource=%t dbStatus=%q dbLimit=%d", mode, source, dbSource, dbStatus, dbLimit)
	}
	if outputPath != "results/login.jsonl" {
		t.Fatalf("outputPath = %q", outputPath)
	}
}

func TestRunInteractiveWizardOAuthUsesExistingAccountAndSkipsBrowserPrompts(t *testing.T) {
	stdin := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"3",
		"",
		"user@example.com",
		"secret-password",
		"",
		"1",
		"",
		"results/oauth.jsonl",
	}, "\n") + "\n"))
	mode := "register"
	accountsPath := ""
	generateCount := 0
	emailDomain := "k9ray.com"
	passwordLength := 14
	backend := ""
	proxyURL := ""
	outputPath := "results/register_results.jsonl"
	screenshotDir := "screenshots"
	workers := 1
	headless := true
	includeSecrets := true

	dbSource := false
	dbStatus := ""
	dbLimit := 0

	accounts, source, err := runInteractiveWizard(stdin, false, &mode, &accountsPath, &generateCount, &emailDomain, &passwordLength, &backend, &proxyURL, &outputPath, &screenshotDir, &workers, &headless, &includeSecrets, &dbSource, &dbStatus, &dbLimit)
	if err != nil {
		t.Fatalf("runInteractiveWizard() error = %v", err)
	}
	if mode != "oauth" {
		t.Fatalf("mode = %q, want oauth", mode)
	}
	if source != "single" {
		t.Fatalf("source = %q, want single", source)
	}
	if len(accounts) != 1 || accounts[0].Email != "user@example.com" || accounts[0].Password != "secret-password" {
		t.Fatalf("accounts = %#v", accounts)
	}
	if generateCount != 0 {
		t.Fatalf("generateCount = %d, want 0", generateCount)
	}
	if backend != "" {
		t.Fatalf("backend = %q, want empty", backend)
	}
	if outputPath != "results/oauth.jsonl" {
		t.Fatalf("outputPath = %q, want results/oauth.jsonl", outputPath)
	}
	if screenshotDir != "screenshots" {
		t.Fatalf("screenshotDir = %q, want unchanged", screenshotDir)
	}
}
