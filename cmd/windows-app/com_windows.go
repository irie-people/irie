package main

import (
	"fmt"
	"log"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The few WebView2 calls the window makes, by hand: the method's place in
// its interface's table, from Microsoft's WebView2.idl, as go-webview2's
// pkg/edge has them (PROVENANCE.md). Its generated pkg/webview2 is not
// used: three of its callbacks take a Go string, which Windows' callbacks
// can't, so importing it panics at once, and its derived interfaces leave
// out their parents' methods.

// com is a COM interface pointer.
type com uintptr

// slot is the address of method i of c's table.
func (c com) slot(i int) uintptr {
	table := *ptr[uintptr](uintptr(c))
	return *ptr[uintptr](table + uintptr(i)*unsafe.Sizeof(uintptr(0)))
}

// call calls method i of c with args, and gives its HRESULT.
func (c com) call(i int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(c.slot(i), append([]uintptr{uintptr(c)}, args...)...)
	return r
}

func (c com) addRef() {
	if c != 0 {
		c.call(1)
	}
}

func (c com) release() {
	if c != 0 {
		c.call(2)
	}
}

// query asks c for another of its interfaces; 0 when it has none, as an
// older WebView2 lacks a newer one.
func (c com) query(iid string) com {
	g, err := windows.GUIDFromString(iid)
	if err != nil || c == 0 {
		return 0
	}
	var out com
	if hr := c.call(0, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out))); hr != 0 {
		return 0
	}
	return out
}

func failed(hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("HRESULT %#x", uint32(hr))
	}
	return nil
}

// str calls a method that gives a string the caller frees.
func (c com) str(i int) string {
	var p *uint16
	if failed(c.call(i, uintptr(unsafe.Pointer(&p)))) != nil || p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

// out calls a method that gives an interface.
func (c com) out(i int) com {
	var o com
	if failed(c.call(i, uintptr(unsafe.Pointer(&o)))) != nil {
		return 0
	}
	return o
}

// withString calls method i with s as its first argument, a UTF-16 string
// WebView2 copies.
func (c com) withString(i int, s string, rest ...uintptr) uintptr {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return uintptr(windows.E_INVALIDARG)
	}
	r := c.call(i, append([]uintptr{uintptr(unsafe.Pointer(p))}, rest...)...)
	runtime.KeepAlive(p)
	return r
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// Method places.
const (
	// ICoreWebView2Environment
	envCreateController = 3
	envCreateResponse   = 4

	// ICoreWebView2Controller, and Controller2 after it
	ctlPutIsVisible     = 4
	ctlPutBounds        = 6
	ctlMoveFocus        = 12
	ctlNotifyMoved      = 23
	ctlGetCoreWebView2  = 25
	ctl2PutBackground   = 27
	iidController2      = "{c979903e-d4ca-4228-92eb-47ee3fa96eab}"
	focusProgrammatic   = 0
	resourceContextAll  = 0
	permissionAllow     = 1
	permissionDeny      = 2
	permissionClipRead  = 6
	captureFormatPNG    = 0
	failedBrowserExited = 0
	failedRenderExited  = 1
	failedUnresponsive  = 2

	// ICoreWebView2
	wvGetSettings         = 3
	wvNavigate            = 5
	wvAddNavStarting      = 7
	wvAddPermission       = 23
	wvAddProcessFailed    = 25
	wvExecuteScript       = 29
	wvCapturePreview      = 30
	wvReload              = 31
	wvPostString          = 33
	wvAddMessage          = 34
	wvAddNewWindow        = 44
	wvOpenDevTools        = 51
	wvAddResource         = 55
	wvAddResourceFilter   = 57
	webMessageTryAsString = 5

	// ICoreWebView2Settings, and Settings2 to Settings6 after it
	setPutScriptDialogs  = 8
	setPutStatusBar      = 10
	setPutDevTools       = 12
	setPutContextMenus   = 14
	setPutHostObjects    = 16
	setPutZoomControl    = 18
	setPutErrorPage      = 20
	set3PutBrowserKeys   = 24
	set4PutPasswordSave  = 26
	set4PutAutofill      = 28
	set6PutSwipe         = 32
	iidSettings3         = "{fdb5ab74-af33-4854-84f0-0a631deb5eba}"
	iidSettings4         = "{cb56846c-4168-4d53-b04f-03b6d6796ff2}"
	iidSettings6         = "{11cb3acd-9bc8-43b8-83bf-f40753714f87}"
	resArgsGetRequest    = 3
	resArgsPutResponse   = 5
	requestGetURI        = 3
	navArgsGetURI        = 3
	navArgsPutCancel     = 8
	newWinArgsGetURI     = 3
	newWinArgsPutHandled = 6
	permArgsGetKind      = 4
	permArgsPutState     = 7
	failArgsGetKind      = 3
)

// putBounds gives the web view its place in the window. A RECT goes by
// reference on x64 and in two registers on ARM64, as each one's calling
// convention passes a 16-byte struct.
func putBounds(ctl com, r rect) {
	if runtime.GOARCH == "arm64" {
		w := (*[2]uintptr)(unsafe.Pointer(&r))
		ctl.call(ctlPutBounds, w[0], w[1])
		return
	}
	ctl.call(ctlPutBounds, uintptr(unsafe.Pointer(&r)))
}

// ---- Handlers -------------------------------------------------------------------

// handler is a COM object of the app's own that WebView2 calls back: one
// Invoke, whatever its interface, since every handler the app needs takes
// at most two arguments after itself.
type handler struct {
	table  *[4]uintptr
	invoke func(a, b uintptr) uintptr
}

var (
	handlerTable = [4]uintptr{
		windows.NewCallback(func(this, iid, out uintptr) uintptr {
			*ptr[uintptr](out) = this
			return 0
		}),
		windows.NewCallback(func(this uintptr) uintptr { return 1 }),
		windows.NewCallback(func(this uintptr) uintptr { return 1 }),
		windows.NewCallback(func(this, a, b uintptr) uintptr {
			if h := handlers[this]; h != nil {
				return h.invoke(a, b)
			}
			return 0
		}),
	}
	// handlers keeps each handler for as long as the app runs, and finds it
	// when it is called. Only the window's thread touches it.
	handlers = map[uintptr]*handler{}
)

// newHandler makes a handler that runs f.
func newHandler(f func(a, b uintptr) uintptr) uintptr {
	h := &handler{table: &handlerTable, invoke: f}
	p := uintptr(unsafe.Pointer(h))
	handlers[p] = h
	return p
}

// on adds an event handler with method i of c.
func (c com) on(i int, f func(sender, args com)) {
	var token int64
	h := newHandler(func(a, b uintptr) uintptr {
		f(com(a), com(b))
		return 0
	})
	if err := failed(c.call(i, h, uintptr(unsafe.Pointer(&token)))); err != nil {
		log.Printf("WebView2 took no handler at %d: %v", i, err)
	}
}
