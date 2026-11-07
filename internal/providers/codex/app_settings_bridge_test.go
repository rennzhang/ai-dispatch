package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	stdRuntime "runtime"
	"strings"
	"testing"
	"time"
)

// Execute the embedded script in the platform JS engine with an entirely fake
// AX bridge. This catches JS null/reference errors without reading any GUI.
func TestDesktopSettingsScriptUsesBoundAXHandlesAndReportsMissingControls(t *testing.T) {
	if stdRuntime.GOOS != "darwin" {
		t.Skip("Desktop settings use macOS JavaScript for Automation")
	}
	engine, err := exec.LookPath("osascript")
	if err != nil {
		t.Skip("macOS JavaScript engine unavailable")
	}
	for _, mode := range []string{"matching", "change-effort", "stalled-effort", "collection-empty-menu", "popup-press-does-not-close", "hidden-menu-residue", "wrong-focused-window", "attribute-aliases", "ambiguous-matching-windows", "missing-window", "missing-selector", "wrong-owner"} {
		t.Run(mode, func(t *testing.T) {
			source, _ := json.Marshal(desktopSettingsScript)
			fixture := fmt.Sprintf(`
var mode=%q, keyboard=0, clock=0, menuOpen=false, menuEverOpened=false, fast=false;
var level=(mode==="change-effort" || mode==="stalled-effort" || mode==="collection-empty-menu" || mode==="popup-press-does-not-close" || mode==="hidden-menu-residue" || mode==="wrong-focused-window")?2:3;
var popup={role:"AXPopUpButton"};
var window={elementID:1,role:"AXWindow",title:"Test thread",pid:mode==="wrong-owner"?99:42};
var alias={elementID:1,role:"AXWindow",title:"Test thread",pid:window.pid};
var distinct={elementID:2,role:"AXWindow",title:"Test thread",pid:42};
var menu={role:"AXGroup",title:"选择推理强度"};
var strength={role:"AXMenuItem",title:"强度"};
var current={role:"AXStaticText"};
var speed={role:"AXMenuItem",title:"启用快速模式"};
var app={kind:"app"};
var running={bundleIdentifier:"com.openai.codex",bundleURL:{path:"/test/ChatGPT.app"}};
var fakeObjC={import:function(){},bindFunction:function(){},unwrap:function(value){return value;},castRefToObject:function(value){return value;},castObjectToRef:function(value){return value;}};
var bridge=function(value){return value;};
bridge.NSRunningApplication={runningApplicationWithProcessIdentifier:function(pid){if(pid!==42)throw Error("wrong requested PID");return running;}};
bridge.AXUIElementCreateApplication=function(pid){if(pid!==42)throw Error("wrong AX application PID");return app;};
bridge.AXUIElementGetPid=function(element,out){out[0]=element.pid;return 0;};
bridge.CFEqual=function(a,b){return a.elementID===b.elementID;};
bridge.AXUIElementCopyAttributeValue=function(element,name,out){
 if(element===app && name==="AXWindows"){
  var list=mode==="missing-window" || (mode==="collection-empty-menu" && menuOpen)?[]:[window];
  if(mode==="ambiguous-matching-windows")list=[window,distinct];
  out[0]={js:list};return 0;
 }
 if(element===app && (name==="AXFocusedWindow" || name==="AXMainWindow") && mode!=="missing-window"){
  out[0]=mode==="wrong-focused-window" && name==="AXFocusedWindow"?{elementID:2,role:"AXWindow",title:"Other thread",pid:42}:alias;return 0;
 }
 if(name==="AXExpanded" && element===popup){out[0]=menuOpen;return 0;}
 if(name==="AXHidden"){out[0]=mode==="hidden-menu-residue" && menuEverOpened && !menuOpen;return 0;}
 if(name==="AXTitle" && element===popup){out[0]=menuOpen?"选择推理强度":"GPT-6.1 Sol "+(level===2?"中":"高");return 0;}
 if(name==="AXTitle" && element.title!==undefined){out[0]=element.title;return 0;}
 if(name==="AXValue" && element===current){out[0]="GPT-6.1 Sol "+(level===2?"标准":"深度")+"，第 "+level+" 项，共 6 项。";return 0;}
 if(name==="AXValue" && element===speed){out[0]=fast?1:0;return 0;}
 if(name==="AXRole" && element.role!==undefined){out[0]=element.role;return 0;}
 if(name==="AXChildren"){
  var children=[];
  if(element.elementID===1 && mode!=="missing-selector")children=menuOpen || (mode==="hidden-menu-residue" && menuEverOpened)?[popup,menu]:[popup];
  if(element===menu)children=[strength,current,speed];
  out[0]={js:children};return 0;
 }
 return -25212;
};
bridge.AXUIElementPerformAction=function(element,action){if(action!=="AXPress")throw Error("unexpected action");if(element===popup){if(!(mode==="popup-press-does-not-close" && menuOpen))menuOpen=!menuOpen;menuEverOpened=true;}else if(element===speed)fast=!fast;return 0;};
bridge.CGEventCreateKeyboardEvent=function(source,key,down){return {key:key,down:down};};
bridge.CGEventSetFlags=function(){};
bridge.CGEventPostToPid=function(pid,event){if(pid!==42)throw Error("keyboard drifted to another PID");keyboard++;if(event.down && event.key===53)menuOpen=false;else if(event.down && mode!=="stalled-effort")level+=event.key===124?1:-1;};
var runScript=new Function("ObjC","$","Ref","Application","delay","Date",%s+"\nreturn run([JSON.stringify({pid:42,bundle:'/test/ChatGPT.app',title:'Test thread',model:'gpt-6.1-sol',effort:'high',fast:false})]);");
var result;
try{result={ok:true,receipt:JSON.parse(runScript(fakeObjC,bridge,function(){return [];},function(){throw Error("global keyboard/GUI lookup is forbidden");},function(){if(mode==="missing-window"||mode==="missing-selector"||mode==="wrong-owner"||mode==="ambiguous-matching-windows"||mode==="wrong-focused-window")clock+=5000;else if(mode==="stalled-effort")clock+=1000;},{now:function(){return clock;}})),keyboard:keyboard};}
catch(error){result={ok:false,error:String(error),keyboard:keyboard};}
JSON.stringify(result);`, mode, source)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			raw, err := exec.CommandContext(ctx, engine, "-l", "JavaScript", "-e", fixture).CombinedOutput()
			if err != nil {
				t.Fatalf("script fixture failed: %v %s", err, raw)
			}
			var result struct {
				OK       bool   `json:"ok"`
				Error    string `json:"error"`
				Keyboard int    `json:"keyboard"`
				Receipt  struct {
					PID    int    `json:"pid"`
					Preset string `json:"preset"`
				} `json:"receipt"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatalf("invalid fixture receipt: %v %s", err, raw)
			}
			wantKeys := 0
			if mode == "change-effort" || mode == "collection-empty-menu" || mode == "popup-press-does-not-close" || mode == "hidden-menu-residue" {
				wantKeys = 4
			}
			if mode == "stalled-effort" {
				wantKeys = 6
			}
			if result.Keyboard != wantKeys {
				t.Fatalf("targeted keys=%d want=%d", result.Keyboard, wantKeys)
			}
			if mode == "matching" || mode == "change-effort" || mode == "collection-empty-menu" || mode == "popup-press-does-not-close" || mode == "hidden-menu-residue" || mode == "attribute-aliases" {
				if !result.OK || result.Receipt.PID != 42 || result.Receipt.Preset != "GPT-6.1 Sol 高" {
					t.Fatalf("result=%+v", result)
				}
				return
			}
			if mode == "ambiguous-matching-windows" && !strings.Contains(result.Error, `"matches":2`) {
				t.Fatalf("distinct matching windows not rejected: %+v", result)
			}
			if result.OK || result.Error == "" || strings.Contains(result.Error, "TypeError") || !(strings.Contains(result.Error, "no input was queued") || strings.Contains(result.Error, "no key or input was sent")) {
				t.Fatalf("missing UI did not fail clearly: %+v", result)
			}
		})
	}
}
