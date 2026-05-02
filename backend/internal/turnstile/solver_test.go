package turnstile

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeBrowser struct {
	token     string
	evalCalls int
}

func (b *fakeBrowser) Navigate(string) error                   { return nil }
func (b *fakeBrowser) Type(string, string) error               { return nil }
func (b *fakeBrowser) Click(string) error                      { return nil }
func (b *fakeBrowser) WaitVisible(string, time.Duration) error { return nil }
func (b *fakeBrowser) Close(context.Context)                   {}
func (b *fakeBrowser) Eval(script string, out any) error {
	b.evalCalls++
	if out != nil {
		if target, ok := out.(*string); ok {
			*target = b.token
		}
	}
	return nil
}

func TestSolveWithBrowserReadsCapturedToken(t *testing.T) {
	browser := &fakeBrowser{token: strings.Repeat("a", 60)}
	result, err := SolveWithBrowser(context.Background(), browser, Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Token != browser.token {
		t.Fatalf("token = %q", result.Token)
	}
	if browser.evalCalls < 2 {
		t.Fatalf("expected install and poll eval calls, got %d", browser.evalCalls)
	}
}
