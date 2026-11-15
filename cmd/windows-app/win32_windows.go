package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Win32 calls the window makes, from Microsoft's documentation of each
// (PROVENANCE.md). Each is looked up as it is first used.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	shlwapi  = windows.NewLazySystemDLL("shlwapi.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pUpdateWindow                  = user32.NewProc("UpdateWindow")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pSendMessageW                  = user32.NewProc("SendMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pSetWindowTextW                = user32.NewProc("SetWindowTextW")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pGetWindowPlacement            = user32.NewProc("GetWindowPlacement")
	pSetWindowPlacement            = user32.NewProc("SetWindowPlacement")
	pFindWindowW                   = user32.NewProc("FindWindowW")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pIsIconic                      = user32.NewProc("IsIconic")
	pFlashWindowEx                 = user32.NewProc("FlashWindowEx")
	pCreateIconFromResourceEx      = user32.NewProc("CreateIconFromResourceEx")
	pOpenClipboard                 = user32.NewProc("OpenClipboard")
	pEmptyClipboard                = user32.NewProc("EmptyClipboard")
	pSetClipboardData              = user32.NewProc("SetClipboardData")
	pCloseClipboard                = user32.NewProc("CloseClipboard")
	pMessageBoxW                   = user32.NewProc("MessageBoxW")
	pGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	pMonitorFromRect               = user32.NewProc("MonitorFromRect")
	pGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	pMonitorFromPoint              = user32.NewProc("MonitorFromPoint")

	pGlobalAlloc   = kernel32.NewProc("GlobalAlloc")
	pGlobalLock    = kernel32.NewProc("GlobalLock")
	pGlobalUnlock  = kernel32.NewProc("GlobalUnlock")
	pGlobalFree    = kernel32.NewProc("GlobalFree")
	pGetModuleHand = kernel32.NewProc("GetModuleHandleW")

	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	pShellExecuteW    = shell32.NewProc("ShellExecuteW")
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	pSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")

	pCoInitializeEx = ole32.NewProc("CoInitializeEx")
)

// Window messages and the like.
const (
	wmDestroy         = 0x0002
	wmMove            = 0x0003
	wmSize            = 0x0005
	wmActivate        = 0x0006
	wmSetFocus        = 0x0007
	wmClose           = 0x0010
	wmQuit            = 0x0012
	wmGetMinMaxInfo   = 0x0024
	wmSetIcon         = 0x0080
	wmSettingChange   = 0x001A
	wmMoving          = 0x0216
	wmDpiChanged      = 0x02E0
	wmApp             = 0x8000
	wmPost            = wmApp + 1 // the page has messages waiting
	wmTray            = wmApp + 2 // the notification area's icon
	wmUser            = 0x0400
	ninBalloonClick   = wmUser + 5
	wmLButtonUp       = 0x0202
	waInactive        = 0
	swShow            = 5
	swShowNormal      = 1
	swShowMaximized   = 3
	swRestore         = 9
	cwUseDefault      = 0x80000000
	wsOverlappedWin   = 0x00CF0000
	wsClipChildren    = 0x02000000
	idcArrow          = 32512
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	iconSmall         = 0
	iconBig           = 1
	smCxScreen        = 0
	smCyScreen        = 1
	monitorNull       = 0
	monitorNearest    = 2
	gmemMoveable      = 0x0002
	cfUnicodeText     = 13
	mbIconWarning     = 0x30
	mbYesNo           = 0x4
	idYes             = 6
	flashwTray        = 0x2
	flashwTimerNoFG   = 0xC
	nimAdd            = 0
	nimModify         = 1
	nimDelete         = 2
	nimSetVersion     = 4
	nifMessage        = 0x1
	nifIcon           = 0x2
	nifTip            = 0x4
	nifInfo           = 0x10
	niifUser          = 0x4
	niifLargeIcon     = 0x20
	coinitApartment   = 0x2
	dwmDarkMode       = 20 // DWMWA_USE_IMMERSIVE_DARK_MODE
	dwmDarkModeOld    = 19 // its number before Windows 10 20H1
	dwmBorderColor    = 34 // DWMWA_BORDER_COLOR, Windows 11
	dwmCaptionColor   = 35 // DWMWA_CAPTION_COLOR
	dwmTextColor      = 36 // DWMWA_TEXT_COLOR
	dpiPerMonitorV2   = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, -4
	notifyIconVersion = 4
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point
}

type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    point
	MaxPosition    point
	NormalPosition rect
}

type flashWInfo struct {
	Size    uint32
	Hwnd    windows.HWND
	Flags   uint32
	Count   uint32
	Timeout uint32
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

// notifyIconData is NOTIFYICONDATAW as Windows Vista and later have it.
type notifyIconData struct {
	Size            uint32
	Wnd             windows.HWND
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32 // also uTimeout
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        windows.GUID
	BalloonIcon     windows.Handle
}

// ptr reads a pointer that a Win32 call gave as a number, such as a
// message's lParam or GlobalLock's answer. The memory is Windows', not
// Go's, so the collector never moves it.
func ptr[T any](p uintptr) *T { return *(**T)(unsafe.Pointer(&p)) }

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// copyUTF16 writes s into dst, cut to fit with its NUL.
func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

func clientRect(hwnd windows.HWND) rect {
	var r rect
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

func dpiOf(hwnd windows.HWND) int {
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(uintptr(hwnd)); d > 0 {
			return int(d)
		}
	}
	return 96
}

func systemDPI() int {
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d > 0 {
			return int(d)
		}
	}
	return 96
}

// colorRef is "#RRGGBB" as a COLORREF, 0x00BBGGRR; ok is false for
// anything else.
func colorRef(hex string) (uint32, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, false
	}
	var v uint32
	for _, c := range hex[1:] {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint32(c-'A') + 10
		default:
			return 0, false
		}
	}
	r, g, b := v>>16&0xff, v>>8&0xff, v&0xff
	return b<<16 | g<<8 | r, true
}

func dwmSet(hwnd windows.HWND, attr uint32, v uint32) bool {
	if pDwmSetWindowAttribute.Find() != nil {
		return false
	}
	hr, _, _ := pDwmSetWindowAttribute.Call(uintptr(hwnd), uintptr(attr), uintptr(unsafe.Pointer(&v)), 4)
	return hr == 0
}

// setClipboard puts text on the clipboard as Unicode text.
func setClipboard(hwnd windows.HWND, text string) bool {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return false
	}
	if r, _, _ := pOpenClipboard.Call(uintptr(hwnd)); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	size := uintptr(len(u) * 2)
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return false
	}
	copy(unsafe.Slice(ptr[uint16](p), len(u)), u)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return false
	}
	return true
}

func messageBox(hwnd windows.HWND, title, text string, flags uintptr) uintptr {
	r, _, _ := pMessageBoxW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), flags)
	return r
}

// openURL opens an address in the default browser; only web addresses.
func openURL(u string) {
	if len(u) < 8 || (u[:8] != "https://" && u[:7] != "http://") {
		return
	}
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(u))), 0, 0, swShowNormal)
}
