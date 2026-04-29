package email

import (
	"testing"
	"time"
)

func TestExtractEmailCodes(t *testing.T) {
	matches := ExtractEmailCodes(Message{Body: "Your verification code is 123456."}, nil)
	if len(matches) != 1 || matches[0].Value != "123456" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestKeywordMatchesWholeASCIIWord(t *testing.T) {
	if !KeywordMatches("Your verification code", "code") {
		t.Fatal("expected code to match")
	}
	if KeywordMatches("encoded value", "code") {
		t.Fatal("did not expect code to match encoded")
	}
}

func TestVerificationCodeStoreLatestUsedCode(t *testing.T) {
	store := NewVerificationCodeStore(t.TempDir() + "/codes.jsonl")
	err := store.Save(StoredCode{Recipient: "a@example.com", Subject: "verification code", Code: "654321", UsedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.LatestUsedCode("a@example.com", time.Minute, "", []string{"verification"}, nil); got != "654321" {
		t.Fatalf("latest code = %q", got)
	}
}
