package sub2api

import "testing"

func TestValidatePersonalTokenBinding(t *testing.T) {
	binding := TokenBinding{ChatGPTAccountID: "acct", PlanType: "personal/plus"}
	if err := ValidatePersonalTokenBinding(binding, "team"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePersonalTokenBinding(binding, "acct"); err == nil {
		t.Fatal("expected team workspace binding to fail")
	}
}

func TestInterpretResponse(t *testing.T) {
	result := InterpretResponse(map[string]any{"status": "ok", "data": map[string]any{"id": "42"}})
	if !result.Success || result.RecordID != "42" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
