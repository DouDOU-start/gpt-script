package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreatePlannedRunWritesAccountAndEvents(t *testing.T) {
	dir := t.TempDir()
	ctx, err := CreatePlannedRun("register", []PlannedAccount{{Index: 0, Email: "a@example.com", Password: "secret", Name: "A"}}, dir, map[string]any{"proxy_enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"manifest.json", "summary.json", "events.jsonl", "accounts/0000/account.json", "accounts/0000/events.jsonl"} {
		if _, err := os.Stat(filepath.Join(ctx.RunDir, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	store := StoreFromContext(*ctx)
	if err := store.UpdateAccount(0, map[string]any{"oauth": map[string]any{"status": "done"}}); err != nil {
		t.Fatal(err)
	}
	account, err := store.LoadAccount(0)
	if err != nil {
		t.Fatal(err)
	}
	oauth, _ := account["oauth"].(map[string]any)
	if oauth["status"] != "done" {
		t.Fatalf("unexpected oauth status: %#v", oauth["status"])
	}
}
