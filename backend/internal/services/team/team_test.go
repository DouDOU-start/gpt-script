package team

import "testing"

func TestValidateSeatType(t *testing.T) {
	if _, err := ValidateSeatType("usage_based"); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSeatType("owner"); err == nil {
		t.Fatal("expected invalid seat type")
	}
}

func TestPlanTypeClassification(t *testing.T) {
	if !IsTeamPlanType("team_plus") {
		t.Fatal("expected team plan")
	}
	if !IsPersonalPlanType("personal/pro") {
		t.Fatal("expected personal plan")
	}
}
