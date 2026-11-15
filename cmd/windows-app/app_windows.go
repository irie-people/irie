package main

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/version"
	"github.com/irie-people/irie/internal/winapp"
)

// The window's thread is the process's first, which COM and the window's
// messages need to stay on.
func init() { runtime.LockOSThread() }

//go:embed Irie.ico
var icoFile []byte

const className = "IrieWindow"

// app is the window and what it holds.
type app struct {
	hwnd     windows.HWND
	view     *webView
	backend  *winapp.Backend
	dir      string // %LOCALAPPDATA%\irie
	snapshot string // --snapshot's folder

	mu      sync.Mutex
	queue   []string // messages for the page, waiting for the window's thread
	calls   []func() // work for the window's thread
	posting bool

	trayOn bool
	icons  struct{ big, small windows.Handle }
}

var the *app

func run(args []string) int {
	fs := flag.NewFlagSet(brand.Exe, flag.ContinueOnError)
	snapshot := fs.String("snapshot", "", "draw the window into this folder as PNGs once its sessions show, then quit")
	showVersion := fs.Bool("version", false, "print the version and quit")
	if fs.Parse(args) != nil {
		return 2
	}
	if *showVersion {
		fmt.Println(brand.Exe+".exe", version.String())
		return 0
	}
	// Each monitor's own scale, so text is sharp on every screen.
	if pSetProcessDpiAwarenessContext.Find() == nil {
		pSetProcessDpiAwarenessContext.Call(dpiPerMonitorV2)
	} else if pSetProcessDPIAware.Find() == nil {
		pSetProcessDPIAware.Call()
	}
	a := &app{dir: filepath.Join(os.Getenv("LOCALAPPDATA"), brand.Slug), snapshot: *snapshot}
	os.MkdirAll(a.dir, 0o755)
	a.openLog()
	log.Printf("%s.exe %s starting", brand.Exe, version.String())

	// One window: a second start shows the first.
	if a.snapshot == "" {
		name := utf16("Local\\" + brand.Name + "-App")
		h, err := windows.CreateMutex(nil, false, name)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			if w, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(utf16(className))), 0); w != 0 {
				if r, _, _ := pIsIconic.Call(w); r != 0 {
					pShowWindow.Call(w, swRestore)
				}
				pSetForegroundWindow.Call(w)
			}
			return 0
		}
		defer windows.CloseHandle(h)
	}

	if err := comInit(); err != nil {
		log.Print("COM: ", err)
	}
	if !webView2Present() {
		const get = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
		log.Print("no WebView2 runtime")
		if a.snapshot != "" {
			return 1
		}
		if messageBox(0, brand.Name, brand.Name+" draws its window with Microsoft Edge WebView2, which this Windows does not have yet. It comes with Windows 11 and most of Windows 10.\n\nGet it from Microsoft now?", mbIconWarning|mbYesNo) == idYes {
			openURL(get)
		}
		return 1
	}
	the = a
	a.backend = winapp.New(a, newMachine(a.dir))
	if err := a.makeWindow(); err != nil {
		log.Print("window: ", err)
		a.problem(brand.Name + " could not open its window: " + err.Error())
		return 1
	}
	view, err := newWebView(a)
	if err != nil {
		log.Print("webview: ", err)
		a.problem(brand.Name + " could not start WebView2: " + err.Error())
		return 1
	}
	a.view = view
	if a.snapshot != "" {
		// A snapshot that never shows its sessions fails rather than hang.
		go func() {
			time.Sleep(90 * time.Second)
			log.Print("snapshot: nothing showed in 90 s")
			os.Exit(1)
		}()
	}
	return a.loop()
}

// problem tells the person what went wrong, unless no one is there to
// read it, as for a snapshot.
func (a *app) problem(text string) {
	if a.snapshot == "" {
		messageBox(a.hwnd, brand.Name, text, mbIconWarning)
	}
}

func comInit() error {
	hr, _, _ := pCoInitializeEx.Call(0, coinitApartment)
	if int32(hr) < 0 {
		return fmt.Errorf("CoInitializeEx: %#x", hr)
	}
	return nil
}

// openLog keeps what the app says in app.log, the last run's only.
func (a *app) openLog() {
	f, err := os.OpenFile(filepath.Join(a.dir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

// ---- The window --------------------------------------------------------------

func (a *app) makeWindow() error {
	var inst windows.Handle
	windows.GetModuleHandleEx(0, nil, &inst)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	dark := windowsDark()
	ground := uint32(0x00DFEBE9) // Evergreen's ground, light, as a COLORREF
	if dark {
		ground = 0x0010140E
	}
	brush, _, _ := pCreateSolidBrush.Call(uintptr(ground))
	a.icons.big, a.icons.small = loadIcons(systemDPI())
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    windows.NewCallback(wndProc),
		Instance:   inst,
		Icon:       a.icons.big,
		IconSm:     a.icons.small,
		Cursor:     windows.Handle(cursor),
		Background: windows.Handle(brush),
		ClassName:  utf16(className),
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}
	// 1280 by 820 at 100%, in the middle of the screen, unless a placement
	// was kept from the last time.
	dpi := systemDPI()
	w, h := 1280*dpi/96, 820*dpi/96
	sw, _, _ := pGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := pGetSystemMetrics.Call(smCyScreen)
	w, h = min(w, int(sw)*9/10), min(h, int(sh)*9/10)
	x, y := (int(sw)-w)/2, (int(sh)-h)/2
	if a.snapshot != "" {
		// The same picture on every screen, a CI runner's small one too.
		x, y, w, h = 0, 0, 1280*dpi/96, 800*dpi/96
	}
	hwnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16(className))), uintptr(unsafe.Pointer(utf16(brand.Name))),
		wsOverlappedWin|wsClipChildren, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, uintptr(inst), 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %v", err)
	}
	a.hwnd = windows.HWND(hwnd)
	a.look(winapp.Request{Dark: dark})
	show := uintptr(swShow)
	if a.snapshot == "" {
		if p, ok := a.loadPlacement(); ok {
			pSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p)))
			show = uintptr(p.ShowCmd)
		}
	}
	pShowWindow.Call(hwnd, show)
	pUpdateWindow.Call(hwnd)
	return nil
}

func (a *app) loop() int {
	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return 0
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd windows.HWND, message uint32, wparam, lparam uintptr) uintptr {
	a := the
	if a != nil && a.hwnd == hwnd {
		switch message {
		case wmPost:
			a.flush()
			return 0
		case wmSize:
			if a.view != nil {
				a.view.resize()
			}
		case wmMove, wmMoving:
			if a.view != nil {
				a.view.moved()
			}
		case wmActivate:
			if wparam&0xffff != waInactive {
				if a.view != nil {
					a.view.focus()
				}
				a.trayOff()
			}
		case wmDpiChanged:
			// The new monitor's scale: the size Windows suggests for it.
			r := ptr[rect](lparam)
			pSetWindowPos.Call(uintptr(hwnd), 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
			big, small := loadIcons(int(wparam & 0xffff))
			pSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconBig, uintptr(big))
			pSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconSmall, uintptr(small))
			return 0
		case wmGetMinMaxInfo:
			mm := ptr[minMaxInfo](lparam)
			dpi := int32(dpiOf(hwnd))
			mm.MinTrackSize = point{X: 720 * dpi / 96, Y: 460 * dpi / 96}
			return 0
		case wmTray:
			switch lparam & 0xffff {
			case ninBalloonClick, wmLButtonUp:
				a.toFront()
			}
			return 0
		case wmClose:
			a.savePlacement()
			a.trayOff()
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case wmDestroy:
			pPostQuitMessage.Call(0)
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
	return r
}

func (a *app) toFront() {
	if r, _, _ := pIsIconic.Call(uintptr(a.hwnd)); r != 0 {
		pShowWindow.Call(uintptr(a.hwnd), swRestore)
	}
	pSetForegroundWindow.Call(uintptr(a.hwnd))
}

// placement is where the window was left, kept in window.json.
type placement struct {
	Left, Top, Right, Bottom int32
	Maximized                bool
}

func (a *app) placementFile() string { return filepath.Join(a.dir, "window.json") }

func (a *app) savePlacement() {
	if a.snapshot != "" {
		return
	}
	p := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if r, _, _ := pGetWindowPlacement.Call(uintptr(a.hwnd), uintptr(unsafe.Pointer(&p))); r == 0 {
		return
	}
	n := p.NormalPosition
	b, _ := json.Marshal(placement{n.Left, n.Top, n.Right, n.Bottom, p.ShowCmd == swShowMaximized})
	os.WriteFile(a.placementFile(), b, 0o644)
}

func (a *app) loadPlacement() (windowPlacement, bool) {
	b, err := os.ReadFile(a.placementFile())
	if err != nil {
		return windowPlacement{}, false
	}
	var k placement
	if json.Unmarshal(b, &k) != nil || k.Right-k.Left < 200 || k.Bottom-k.Top < 150 {
		return windowPlacement{}, false
	}
	// Only where a monitor still is.
	r := rect{k.Left, k.Top, k.Right, k.Bottom}
	if mon, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorNull); mon == 0 {
		return windowPlacement{}, false
	}
	show := uint32(swShowNormal)
	if k.Maximized {
		show = swShowMaximized
	}
	return windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{})), ShowCmd: show, NormalPosition: r}, true
}

// windowsDark says whether Windows draws its apps dark.
func windowsDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// loadIcons makes the window's icons from Irie.ico's sizes nearest the
// monitor's: a big one for Alt+Tab and the taskbar, a small one for the
// title bar.
func loadIcons(dpi int) (big, small windows.Handle) {
	return iconOf(32 * dpi / 96), iconOf(16 * dpi / 96)
}

func iconOf(px int) windows.Handle {
	img, side := icoEntry(icoFile, px)
	if img == nil {
		return 0
	}
	h, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)), 1, 0x00030000, uintptr(side), uintptr(side), 0)
	return windows.Handle(h)
}

// icoEntry is the image of the smallest size in an .ico at least px wide,
// or its largest.
func icoEntry(ico []byte, px int) ([]byte, int) {
	if len(ico) < 6 {
		return nil, 0
	}
	n := int(binary.LittleEndian.Uint16(ico[4:6]))
	var best []byte
	bestSide := 0
	for k := 0; k < n && 6+16*(k+1) <= len(ico); k++ {
		e := ico[6+16*k:]
		side := int(e[0])
		if side == 0 {
			side = 256
		}
		size := int(binary.LittleEndian.Uint32(e[8:12]))
		off := int(binary.LittleEndian.Uint32(e[12:16]))
		if off+size > len(ico) {
			continue
		}
		better := best == nil ||
			side >= px && (bestSide < px || side < bestSide) ||
			bestSide < px && side > bestSide
		if better {
			best, bestSide = ico[off:off+size], side
		}
	}
	return best, bestSide
}

// ---- What the page asks of the window ------------------------------------------

// Post is winapp.Host's: the message waits for the window's thread, which
// takes every waiting one at once.
func (a *app) Post(m []byte) {
	a.mu.Lock()
	a.queue = append(a.queue, string(m))
	wake := !a.posting
	a.posting = true
	a.mu.Unlock()
	if wake {
		pPostMessageW.Call(uintptr(a.hwnd), wmPost, 0, 0)
	}
}

// ui runs f on the window's thread, as WebView2 and the window want.
func (a *app) ui(f func()) {
	a.mu.Lock()
	a.calls = append(a.calls, f)
	wake := !a.posting
	a.posting = true
	a.mu.Unlock()
	if wake {
		pPostMessageW.Call(uintptr(a.hwnd), wmPost, 0, 0)
	}
}

// flush posts the waiting messages to the page, as JSON arrays of at most
// about a megabyte each, and runs the waiting work.
func (a *app) flush() {
	a.mu.Lock()
	q, calls := a.queue, a.calls
	a.queue, a.calls = nil, nil
	a.posting = false
	a.mu.Unlock()
	defer func() {
		for _, f := range calls {
			f()
		}
	}()
	if a.view == nil || len(q) == 0 {
		return
	}
	var b strings.Builder
	send := func() {
		if b.Len() > 0 {
			b.WriteByte(']')
			a.view.post(b.String())
			b.Reset()
		}
	}
	for _, m := range q {
		if b.Len() > 0 && b.Len()+len(m) > 1<<20 {
			send()
		}
		if b.Len() == 0 {
			b.WriteByte('[')
		} else {
			b.WriteByte(',')
		}
		b.WriteString(m)
	}
	send()
}

// Do is winapp.Host's: what the page asks of the window itself. It runs on
// the window's thread.
func (a *app) Do(r winapp.Request) {
	switch r.Q {
	case "open":
		openURL(r.URL)
	case "look":
		a.look(r)
	case "title":
		if r.Title != "" {
			pSetWindowTextW.Call(uintptr(a.hwnd), uintptr(unsafe.Pointer(utf16(r.Title))))
		}
	case "notify":
		a.notify(r.Title, r.Body)
	case "copy":
		setClipboard(a.hwnd, r.Body)
	case "devtools":
		if a.view != nil {
			a.view.devTools()
		}
	case "quit":
		pPostMessageW.Call(uintptr(a.hwnd), wmClose, 0, 0)
	case "rendered":
		if a.snapshot != "" && a.view != nil {
			a.view.snapshots(a.snapshot)
		}
	}
}

// look colours the title bar as the page's sidebar is, and dark while
// the page is.
func (a *app) look(r winapp.Request) {
	dark := uint32(0)
	if r.Dark {
		dark = 1
	}
	if !dwmSet(a.hwnd, dwmDarkMode, dark) {
		dwmSet(a.hwnd, dwmDarkModeOld, dark)
	}
	// Windows 11's own colours for the bar; Windows 10 keeps its own.
	if c, ok := colorRef(r.Caption); ok {
		dwmSet(a.hwnd, dwmCaptionColor, c)
	}
	if c, ok := colorRef(r.Text); ok {
		dwmSet(a.hwnd, dwmTextColor, c)
	}
	if c, ok := colorRef(r.Border); ok {
		dwmSet(a.hwnd, dwmBorderColor, c)
	}
}

// notify says that a session needs you while the window is not in front:
// its taskbar button flashes until you come to it, and a notification
// says what it needs.
func (a *app) notify(title, body string) {
	if fg, _, _ := pGetForegroundWindow.Call(); windows.HWND(fg) == a.hwnd {
		return
	}
	f := flashWInfo{Size: uint32(unsafe.Sizeof(flashWInfo{})), Hwnd: a.hwnd, Flags: flashwTray | flashwTimerNoFG}
	pFlashWindowEx.Call(uintptr(unsafe.Pointer(&f)))
	n := a.trayData()
	n.Flags |= nifInfo
	copyUTF16(n.InfoTitle[:], title)
	copyUTF16(n.Info[:], body)
	n.InfoFlags = niifUser | niifLargeIcon
	n.BalloonIcon = a.icons.big
	op := uintptr(nimModify)
	if !a.trayOn {
		op = nimAdd
	}
	if r, _, _ := pShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&n))); r != 0 && !a.trayOn {
		a.trayOn = true
		v := a.trayData()
		v.Version = notifyIconVersion
		pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&v)))
	}
}

func (a *app) trayData() notifyIconData {
	n := notifyIconData{Wnd: a.hwnd, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmTray, Icon: a.icons.small}
	n.Size = uint32(unsafe.Sizeof(n))
	copyUTF16(n.Tip[:], brand.Name)
	return n
}

// trayOff takes the notification area's icon away once you come back.
func (a *app) trayOff() {
	if !a.trayOn {
		return
	}
	n := a.trayData()
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&n)))
	a.trayOn = false
}
