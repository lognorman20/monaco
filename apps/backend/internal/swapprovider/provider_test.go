package swapprovider

import (
	"errors"
	"testing"
)

func TestParseName_defaultsToJupiterAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{"": NameJupiter, "jupiter": NameJupiter, "flash": NameFlash} {
		got, err := ParseName(raw)
		if err != nil || got != want {
			t.Fatalf("ParseName(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := ParseName("uniswap"); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestStageOf_readsStageThroughWrappingAndKeepsCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")
	err := AtStage("order_submit", "req-1", cause)

	stage, requestID := StageOf(err, "fallback")
	if stage != "order_submit" || requestID != "req-1" {
		t.Fatalf("StageOf = %q/%q", stage, requestID)
	}
	if !errors.Is(err, cause) {
		t.Fatal("stage error must unwrap to its cause")
	}
	if stage, _ := StageOf(cause, "fallback"); stage != "fallback" {
		t.Fatalf("untagged stage = %q, want fallback", stage)
	}
	if AtStage("x", "", nil) != nil {
		t.Fatal("AtStage(nil) must stay nil")
	}
}
