package codex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDesktopInstanceSelectionUsesIdentityAndVisibleGUI(t *testing.T) {
	const bundle = "/Applications/ChatGPT.app"
	instances := []desktopInstance{{PID: 10, Bundle: bundle}, {PID: 20, Bundle: bundle, Windows: 1}}
	for _, item := range []struct {
		name      string
		explicit  int
		ancestors []int
		want      int
	}{
		{"visible-gui", 0, nil, 20},
		{"explicit-over-visible", 10, nil, 10},
		{"ancestor-over-visible", 0, []int{99, 10, 1}, 10},
	} {
		t.Run(item.name, func(t *testing.T) {
			selected, err := selectDesktopInstance(instances, item.explicit, item.ancestors)
			if err != nil || selected.PID != item.want {
				t.Fatalf("selected=%+v err=%v", selected, err)
			}
		})
	}
	instances[0].Active = true
	if selected, err := selectDesktopInstance(instances, 0, nil); err != nil || selected.PID != 10 {
		t.Fatalf("active instance not selected: %+v %v", selected, err)
	}
	instances[0].Active = false
	instances[0].Windows = 1
	if _, err := selectDesktopInstance(instances, 0, nil); !errors.Is(err, errDesktopAmbiguous) {
		t.Fatalf("two visible instances must remain ambiguous: %v", err)
	}
	if _, err := selectDesktopInstance(instances, 999, nil); err == nil {
		t.Fatal("stale explicit PID silently selected another instance")
	}
}

func TestDesktopPIDOverrideRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"-1", "0", "unknown", "1.5"} {
		if _, err := desktopExplicitPID(value); err == nil {
			t.Fatalf("invalid PID accepted: %q", value)
		}
	}
}

func TestDesktopDefaultSelectionExcludesCustomProfiles(t *testing.T) {
	instances := []desktopInstance{{PID: 10, CustomProfile: true, Active: true, Windows: 1}, {PID: 20, Windows: 1}, {PID: 30, CustomProfile: true}}
	selected, err := selectDesktopInstance(instances, 0, []int{10})
	if err != nil || selected.PID != 20 {
		t.Fatalf("custom profile stole default routing: %+v %v", selected, err)
	}
	selected, err = selectDesktopInstance(instances, 10, nil)
	if err != nil || selected.PID != 10 {
		t.Fatalf("explicit custom profile was not honored: %+v %v", selected, err)
	}
	for _, args := range []string{"/Applications/ChatGPT --user-data-dir /private/example", "/Applications/ChatGPT --user-data-dir=/private/example"} {
		if !desktopCustomProfile(args) {
			t.Fatal("custom profile flag not recognized")
		}
	}
	if desktopCustomProfile("/Applications/ChatGPT") {
		t.Fatal("standard profile classified as custom")
	}
}

func TestDesktopRouteIsAddressedToBoundPIDAndRejectsDrift(t *testing.T) {
	host, _, _ := desktopBackendFixture(t)
	if !strings.Contains(desktopRouteScript, "descriptorWithProcessIdentifier(request.pid)") || strings.Contains(desktopRouteScript, "URLForApplication") {
		t.Fatal("route does not target a process ID")
	}
	if err := openDesktopThread(context.Background(), host, "thread-42", true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DESKTOP_UI_WRONG_INSTANCE", "1")
	if err := openDesktopThread(context.Background(), host, "thread-42", false); err == nil {
		t.Fatal("route receipt from a different PID accepted")
	}
	if err := openDesktopThread(context.Background(), desktopHost{bundle: host.bundle}, "thread-42", false); err == nil {
		t.Fatal("an unbound host was allowed to open by bundle")
	}
}
