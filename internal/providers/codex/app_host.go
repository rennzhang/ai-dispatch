package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	stdRuntime "runtime"
	"strconv"
	"strings"
	"time"
)

var ErrDesktopNotInstalled = errors.New("Codex Desktop is not installed")

type desktopHost struct {
	bundle string
	binary string
	pid    int
}

type desktopInstance struct {
	PID           int    `json:"pid"`
	Bundle        string `json:"bundle"`
	Active        bool   `json:"active"`
	Windows       int    `json:"windows"`
	CustomProfile bool   `json:"custom_profile"`
}

// Catalog discovery follows the registered installation, not any GUI instance.
func registeredDesktopHost(ctx context.Context) (desktopHost, error) {
	if stdRuntime.GOOS != "darwin" {
		return desktopHost{}, fmt.Errorf("Codex Desktop discovery requires macOS")
	}
	script := `ObjC.import("AppKit"); var url = $.NSWorkspace.sharedWorkspace.URLForApplicationWithBundleIdentifier("com.openai.codex"); JSON.stringify({bundle:url ? ObjC.unwrap(url.path) : ""});`
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
	if err != nil {
		return desktopHost{}, fmt.Errorf("locate registered Codex Desktop: %s", firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
	}
	var location struct {
		Bundle string `json:"bundle"`
	}
	if err := json.Unmarshal(out, &location); err != nil {
		return desktopHost{}, fmt.Errorf("read registered Codex Desktop installation: %w", err)
	}
	return bundledDesktopHost(location.Bundle, 0)
}

func bundledDesktopHost(bundle string, pid int) (desktopHost, error) {
	if bundle == "" {
		return desktopHost{}, ErrDesktopNotInstalled
	}
	for _, relative := range []string{"Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex", "Contents/Resources/codex"} {
		binary := filepath.Join(bundle, relative)
		info, err := os.Stat(binary)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return desktopHost{bundle: bundle, binary: binary, pid: pid}, nil
		}
	}
	return desktopHost{}, fmt.Errorf("Codex Desktop has no executable bundled CLI in %s", bundle)
}

func resolveDesktopHost(ctx context.Context) (desktopHost, error) { return locateDesktopHost(ctx, "") }

func locateDesktopHost(ctx context.Context, wantedBundle string) (desktopHost, error) {
	if stdRuntime.GOOS != "darwin" {
		return desktopHost{}, fmt.Errorf("Codex Desktop delivery requires macOS")
	}
	script := `ObjC.import("AppKit"); JSON.stringify($.NSWorkspace.sharedWorkspace.runningApplications.js.filter(function(a){return ObjC.unwrap(a.bundleIdentifier)==="com.openai.codex";}).map(function(a){return {pid:a.processIdentifier,bundle:ObjC.unwrap(a.bundleURL.path),active:Boolean(a.active)};}));`
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
	if err != nil {
		return desktopHost{}, fmt.Errorf("locate running Codex Desktop: %s", firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
	}
	var instances []desktopInstance
	if err := json.Unmarshal(out, &instances); err != nil {
		return desktopHost{}, fmt.Errorf("read running Codex Desktop identities: %w", err)
	}
	if wantedBundle != "" {
		filtered := instances[:0]
		for _, instance := range instances {
			if instance.Bundle == wantedBundle {
				filtered = append(filtered, instance)
			}
		}
		instances = filtered
	}
	for index := range instances {
		args, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(instances[index].PID), "-o", "args=").Output()
		if err != nil {
			return desktopHost{}, fmt.Errorf("cannot classify a running Codex Desktop profile")
		}
		instances[index].CustomProfile = desktopCustomProfile(string(args))
	}
	explicit, err := desktopExplicitPID(os.Getenv("AI_DISPATCH_CODEX_APP_PID"))
	if err != nil {
		return desktopHost{}, err
	}
	standard := standardDesktopInstances(instances)
	if len(standard) == 0 && explicit == 0 {
		if wantedBundle != "" {
			return bundledDesktopHost(wantedBundle, 0)
		}
		return registeredDesktopHost(ctx)
	}
	ancestors := desktopProcessAncestors(ctx)
	selected, err := selectDesktopInstance(instances, explicit, ancestors)
	if errors.Is(err, errDesktopAmbiguous) {
		if err := readDesktopWindowCounts(ctx, standard); err != nil {
			return desktopHost{}, err
		}
		selected, err = selectDesktopInstance(standard, explicit, ancestors)
	}
	if err != nil {
		return desktopHost{}, err
	}
	return bundledDesktopHost(selected.Bundle, selected.PID)
}

var errDesktopAmbiguous = errors.New("multiple Codex Desktop instances are ambiguous; focus the intended GUI or set AI_DISPATCH_CODEX_APP_PID")

func desktopExplicitPID(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("AI_DISPATCH_CODEX_APP_PID must be a positive process ID")
	}
	return pid, nil
}

func selectDesktopInstance(instances []desktopInstance, explicit int, ancestors []int) (desktopInstance, error) {
	if explicit > 0 {
		for _, instance := range instances {
			if instance.PID == explicit {
				return instance, nil
			}
		}
		return desktopInstance{}, fmt.Errorf("AI_DISPATCH_CODEX_APP_PID does not identify the selected Codex Desktop installation")
	}
	instances = standardDesktopInstances(instances)
	for _, pid := range ancestors {
		for _, instance := range instances {
			if instance.PID == pid {
				return instance, nil
			}
		}
	}
	var active, visible []desktopInstance
	for _, instance := range instances {
		if instance.Active {
			active = append(active, instance)
		}
		if instance.Windows > 0 {
			visible = append(visible, instance)
		}
	}
	if len(active) == 1 {
		return active[0], nil
	}
	if len(visible) == 1 {
		return visible[0], nil
	}
	if len(instances) == 1 {
		return instances[0], nil
	}
	return desktopInstance{}, errDesktopAmbiguous
}

func desktopCustomProfile(args string) bool {
	for _, field := range strings.Fields(args) {
		if field == "--user-data-dir" || strings.HasPrefix(field, "--user-data-dir=") {
			return true
		}
	}
	return false
}

func standardDesktopInstances(instances []desktopInstance) []desktopInstance {
	standard := make([]desktopInstance, 0, len(instances))
	for _, instance := range instances {
		if !instance.CustomProfile {
			standard = append(standard, instance)
		}
	}
	return standard
}

func desktopProcessAncestors(ctx context.Context) []int {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	parents := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		if e1 == nil && e2 == nil {
			parents[pid] = parent
		}
	}
	var ancestors []int
	seen := map[int]bool{}
	for pid := os.Getpid(); pid > 0 && !seen[pid]; pid = parents[pid] {
		ancestors = append(ancestors, pid)
		seen[pid] = true
	}
	return ancestors
}

func readDesktopWindowCounts(ctx context.Context, instances []desktopInstance) error {
	ids := make([]int, 0, len(instances))
	for _, instance := range instances {
		ids = append(ids, instance.PID)
	}
	request, _ := json.Marshal(ids)
	// Do not materialize a whose() array: System Events can turn same-name
	// processes into name-based references pointing at the wrong PID.
	script := `function run(argv) { var system=Application("System Events"); return JSON.stringify(JSON.parse(argv[0]).map(function(pid){var p=system.processes.whose({unixId:pid})[0]; if (p.unixId()!==pid || p.bundleIdentifier()!=="com.openai.codex") throw Error("The requested Desktop process changed"); return {pid:pid,windows:p.windows().length};})); }`
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script, string(request)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot disambiguate Codex Desktop GUI windows; set AI_DISPATCH_CODEX_APP_PID: %s", firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
	}
	var windows []desktopInstance
	if err := json.Unmarshal(out, &windows); err != nil {
		return fmt.Errorf("read Codex Desktop window identities: %w", err)
	}
	if len(windows) != len(instances) {
		return fmt.Errorf("Codex Desktop window discovery returned incomplete identities")
	}
	counts := map[int]int{}
	for _, window := range windows {
		counts[window.PID] = window.Windows
	}
	for index := range instances {
		count, ok := counts[instances[index].PID]
		if !ok {
			return fmt.Errorf("Codex Desktop window discovery lost a requested PID")
		}
		instances[index].Windows = count
	}
	return nil
}

// Only delivery can start the chosen installation. Catalog discovery cannot.
func ensureDesktopRunning(ctx context.Context, host desktopHost) (desktopHost, error) {
	if host.pid > 0 {
		return host, nil
	}
	if out, err := exec.CommandContext(ctx, "open", "-n", "-a", host.bundle).CombinedOutput(); err != nil {
		return desktopHost{}, fmt.Errorf("launch selected Codex Desktop: %s", firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		bound, err := locateDesktopHost(startup, host.bundle)
		if err != nil {
			return desktopHost{}, err
		}
		if bound.pid > 0 {
			return bound, nil
		}
		select {
		case <-startup.Done():
			return desktopHost{}, fmt.Errorf("selected Codex Desktop did not start: %w", startup.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
