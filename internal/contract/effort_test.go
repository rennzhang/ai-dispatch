package contract

import "testing"

func TestNormalizeEffortEmptyIsAuto(t *testing.T) {
	if got := NormalizeEffort(""); got != EffortAuto {
		t.Fatalf("NormalizeEffort(\"\")=%q", got)
	}
	if got := NormalizeEffort("  "); got != EffortAuto {
		t.Fatalf("NormalizeEffort whitespace=%q", got)
	}
	if got := NormalizeEffort(EffortHigh); got != EffortHigh {
		t.Fatalf("NormalizeEffort(high)=%q", got)
	}
}

func TestParseEffortAcceptsAllPublicLevels(t *testing.T) {
	for _, level := range []string{"auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		got, err := ParseEffort(level)
		if err != nil || got != Effort(level) {
			t.Fatalf("ParseEffort(%q)=%q err=%v", level, got, err)
		}
	}
	got, err := ParseEffort("")
	if err != nil || got != EffortAuto {
		t.Fatalf("ParseEffort(\"\")=%q err=%v", got, err)
	}
}

func TestParseEffortRejectsUnknown(t *testing.T) {
	_, err := ParseEffort("ultra")
	if err == nil {
		t.Fatal("expected error")
	}
}
