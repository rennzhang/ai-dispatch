package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type desktopRoute struct {
	PID    int    `json:"pid"`
	Bundle string `json:"bundle"`
	URL    string `json:"url"`
}

// Launch Services' open -a cannot distinguish two processes from the same
// bundle. Address the GURL AppleEvent to the already bound PID instead.
const desktopRouteScript = `function run(argv) {
  ObjC.import("Foundation"); ObjC.import("AppKit");
  var request=JSON.parse(argv[0]);
  var app=$.NSRunningApplication.runningApplicationWithProcessIdentifier(request.pid);
  if (!app || !app.bundleURL || ObjC.unwrap(app.bundleURL.path)!==request.bundle) throw Error("The selected Codex Desktop instance exited or changed; no URL was sent");
  var target=$.NSAppleEventDescriptor.descriptorWithProcessIdentifier(request.pid);
  var event=$.NSAppleEventDescriptor.appleEventWithEventClassEventIDTargetDescriptorReturnIDTransactionID(0x4755524c,0x4755524c,target,-1,0);
  event.setParamDescriptorForKeyword($.NSAppleEventDescriptor.descriptorWithString(request.url),0x2d2d2d2d);
  var error=Ref();
  event.sendEventWithOptionsTimeoutError($.NSAppleEventSendNoReply | $.NSAppleEventSendNeverInteract,10,error);
  if (ObjC.unwrap(error[0])) throw Error(ObjC.unwrap(error[0].localizedDescription));
  return JSON.stringify({pid:request.pid,bundle:request.bundle,url:request.url});
}`

func sendDesktopRoute(ctx context.Context, host desktopHost, route string) error {
	if host.pid <= 0 || host.bundle == "" {
		return fmt.Errorf("open codex desktop thread requires a bound App PID and bundle")
	}
	request := desktopRoute{PID: host.pid, Bundle: host.bundle, URL: route}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", desktopRouteScript, string(payload)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open codex desktop thread in PID %d: %s", host.pid, firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
	}
	var receipt desktopRoute
	if err := json.Unmarshal(out, &receipt); err != nil || receipt != request {
		return fmt.Errorf("open codex desktop thread did not confirm the bound PID/bundle/URL")
	}
	return nil
}
