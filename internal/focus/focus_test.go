package focus

import (
	"reflect"
	"strings"
	"testing"
)

// lsappinfo is the shape of `lsappinfo list` on macOS 14.
const lsappinfo = `Window server:
111) "Finder" ASN:0x0-1001:
    bundleID="com.apple.finder"
    bundle path="/System/Library/CoreServices/Finder.app"
    executable path="/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder"
    pid = 501 type="Foreground" flavor=3 Version="14.1" fileType="APPL" creator="MACS" Arch=ARM64
    parentASN=ASN:0x0-0x1001:

112) "Google Chrome" ASN:0x0-24a84a6: (in front)
    bundleID="com.google.Chrome"
    bundle path="/Applications/Google Chrome.app"
    executable path="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
    pid = 1102 type="Foreground" flavor=3 Version="1" fileType="APPL" creator="????" Arch=ARM64
    parentASN=ASN:0x1-0x2c07:

113) "Music" ASN:0x0-2002:
    bundleID="com.apple.Music"
    pid = 777 type="Foreground"
`

func TestParseFront(t *testing.T) {
	a, ok := ParseFront(lsappinfo)
	want := App{Name: "Google Chrome", BundleID: "com.google.Chrome", BundlePath: "/Applications/Google Chrome.app", PID: 1102}
	if !ok || a != want {
		t.Errorf("front = %+v, %v; want %+v", a, ok, want)
	}
	if _, ok := ParseFront("Window server:\n111) \"Finder\" ASN:0x0-1001: \n    pid = 501\n"); ok {
		t.Error("an app not in front was taken for the front one")
	}
	// The front app's fields end at the next app's line.
	cut := "112) \"Mail\" ASN:0x0-3: (in front) \n    bundleID=\"com.apple.mail\"\n113) \"Other\" ASN:0x0-4: \n    pid = 9\n"
	if a, ok := ParseFront(cut); ok || a.PID == 9 {
		t.Errorf("the next app's pid was taken: %+v", a)
	}
}

func TestParseProcs(t *testing.T) {
	out := `    1     0     0   0.0 /sbin/launchd
 1102     1   501  12.5 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
40169 39945   501 154.1 go
 bad line
`
	want := []Proc{
		{PID: 1, PPID: 0, UID: 0, CPU: 0, Path: "/sbin/launchd"},
		{PID: 1102, PPID: 1, UID: 501, CPU: 12.5, Path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		{PID: 40169, PPID: 39945, UID: 501, CPU: 154.1, Path: "go"},
	}
	if got := ParseProcs(out); !reflect.DeepEqual(got, want) {
		t.Errorf("procs = %+v\nwant %+v", got, want)
	}
}

func TestParseAwake(t *testing.T) {
	out := `Assertion status system-wide:
   PreventUserIdleSystemSleep     1
Listed by owning process:
   pid 99(powerd): [0x0005989200019f4e] 01:28:33 UserIsActive named: "com.apple.powermanagement.lidopen"
   pid 21888(caffeinate): [0x0005ace60001a59a] 00:01:49 PreventUserIdleSystemSleep named: "caffeinate command-line tool"
   pid 312(coreaudiod): [0x0005ace60001a5a0] 00:10:02 PreventUserIdleSystemSleep named: "com.apple.audio.context123.preventuseridlesleep"
	Created for PID: 777.
   pid 900(zoom.us): [0x0005ace60001a5a1] 00:12:00 PreventUserIdleDisplaySleep named: "Zoom meeting"
   pid 55(bluetoothd): [0x0005ace60001a5a2] 00:00:01 ApplePushServiceTask named: "push"
	Created for PID: 66.
`
	want := map[int]bool{21888: true, 312: true, 777: true, 900: true}
	if got := ParseAwake(out); !reflect.DeepEqual(got, want) {
		t.Errorf("awake = %v, want %v", got, want)
	}
}

func TestParseLowPower(t *testing.T) {
	on := "System-wide power settings:\nCurrently in use:\n standby              1\n lowpowermode         1\n hibernatemode        3\n"
	if !ParseLowPower(on) {
		t.Error("Low Power Mode on was read as off")
	}
	if ParseLowPower(strings.Replace(on, "lowpowermode         1", "lowpowermode         0", 1)) || ParseLowPower("") {
		t.Error("Low Power Mode off, or unsaid, was read as on")
	}
}
