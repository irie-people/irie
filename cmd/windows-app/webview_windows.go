package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/webview2"
	"github.com/wailsapp/go-webview2/webviewloader"

	"github.com/irie-people/irie/internal/winapp/web"
)

// webView is the window's WebView2: Microsoft's Edge drawing the page,
// loaded from the app itself (package web) and never from the network,
// whose messages go to the backend and back (docs/WINDOWS.md §5).
type webView struct {
	a     *app
	env   *webview2.ICoreWebView2Environment
	ctl   *webview2.ICoreWebView2Controller
	wv    *webview2.ICoreWebView2
	err   error
	debug bool

	// The handlers WebView2 holds, kept here so the collector leaves them.
	controllerDone *webview2.ICoreWebView2CreateCoreWebView2ControllerCompletedHandler
	messages       *webview2.ICoreWebView2WebMessageReceivedEventHandler
	resources      *webview2.ICoreWebView2WebResourceRequestedEventHandler
	navigations    *webview2.ICoreWebView2NavigationStartingEventHandler
	windows        *webview2.ICoreWebView2NewWindowRequestedEventHandler
	permissions    *webview2.ICoreWebView2PermissionRequestedEventHandler
	failures       *webview2.ICoreWebView2ProcessFailedEventHandler
	captures       []*capture
}

func webView2Present() bool {
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err == nil {
		log.Print("WebView2 ", v)
	}
	return err == nil && v != ""
}

// newWebView makes the window's web view, waiting, with the window's
// messages flowing, until WebView2 has made it.
func newWebView(a *app) (*webView, error) {
	v := &webView{a: a, debug: os.Getenv("IRIE_APP_DEBUG") == "1"}
	data := filepath.Join(a.dir, "WebView2")
	if err := webviewloader.CreateCoreWebView2EnvironmentWithOptions(envDone{v}, webviewloader.WithUserDataFolder(data)); err != nil {
		return nil, err
	}
	var m msg
	for v.ctl == nil && v.err == nil {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return nil, fmt.Errorf("the window closed while WebView2 started")
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return v, v.err
}

// envDone is the environment's completion, as the loader calls it.
type envDone struct{ v *webView }

func (e envDone) EnvironmentCompleted(code webviewloader.HRESULT, env *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	v := e.v
	if code < 0 || env == nil {
		v.err = fmt.Errorf("WebView2's environment failed: %#x", uint32(code))
		return 0
	}
	v.env = (*webview2.ICoreWebView2Environment)(unsafe.Pointer(env))
	v.env.AddRef()
	v.controllerDone = webview2.NewICoreWebView2CreateCoreWebView2ControllerCompletedHandler(v)
	if err := v.env.CreateCoreWebView2Controller(webview2.HWND(v.a.hwnd), v.controllerDone); err != nil {
		v.err = err
	}
	return 0
}

// The handlers' IUnknown: WebView2 holds them for as long as the window
// lives, and so does the app.
func (v *webView) QueryInterface(refiid, object uintptr) uintptr { return 0x80004002 }
func (v *webView) AddRef() uintptr                               { return 1 }
func (v *webView) Release() uintptr                              { return 1 }

func (v *webView) CreateCoreWebView2ControllerCompleted(code uintptr, ctl *webview2.ICoreWebView2Controller) uintptr {
	if int32(code) < 0 || ctl == nil {
		v.err = fmt.Errorf("WebView2's controller failed: %#x", uint32(code))
		return 0
	}
	ctl.AddRef()
	wv, err := ctl.GetCoreWebView2()
	if err != nil {
		v.err = err
		return 0
	}
	wv.AddRef()
	v.wv = wv
	v.settings()
	// The ground the page draws on, so nothing flashes white as it loads.
	if c2 := query[webview2.ICoreWebView2Controller2](unsafe.Pointer(ctl), iidController2); c2 != nil {
		g := webview2.COREWEBVIEW2_COLOR{A: 255, R: 0xE9, G: 0xEB, B: 0xDF}
		if windowsDark() {
			g = webview2.COREWEBVIEW2_COLOR{A: 255, R: 0x0E, G: 0x14, B: 0x10}
		}
		c2.PutDefaultBackgroundColor(g)
	}
	v.messages = webview2.NewICoreWebView2WebMessageReceivedEventHandler(v)
	v.resources = webview2.NewICoreWebView2WebResourceRequestedEventHandler(v)
	v.navigations = webview2.NewICoreWebView2NavigationStartingEventHandler(v)
	v.windows = webview2.NewICoreWebView2NewWindowRequestedEventHandler(v)
	v.permissions = webview2.NewICoreWebView2PermissionRequestedEventHandler(v)
	v.failures = webview2.NewICoreWebView2ProcessFailedEventHandler(v)
	wv.AddWebMessageReceived(v.messages)
	wv.AddWebResourceRequestedFilter(web.Origin+"/*", webview2.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)
	wv.AddWebResourceRequested(v.resources)
	wv.AddNavigationStarting(v.navigations)
	wv.AddNewWindowRequested(v.windows)
	wv.AddPermissionRequested(v.permissions)
	wv.AddProcessFailed(v.failures)
	v.ctl = ctl
	v.resize()
	ctl.PutIsVisible(true)
	if err := wv.Navigate(web.Origin + "/index.html"); err != nil {
		v.err = err
	}
	v.focus()
	return 0
}

// settings keep the page an app's: no browser keys, menus, zoom, status
// bar, autofill or swipes, and no developer tools unless IRIE_APP_DEBUG=1.
func (v *webView) settings() {
	s, err := v.wv.GetSettings()
	if err != nil {
		log.Print("settings: ", err)
		return
	}
	s.PutIsStatusBarEnabled(false)
	s.PutAreDefaultContextMenusEnabled(v.debug)
	s.PutAreDevToolsEnabled(v.debug)
	s.PutIsZoomControlEnabled(false)
	s.PutIsBuiltInErrorPageEnabled(true)
	s.PutAreHostObjectsAllowed(false)
	s.PutAreDefaultScriptDialogsEnabled(false)
	if s3 := query[webview2.ICoreWebView2Settings3](unsafe.Pointer(s), iidSettings3); s3 != nil {
		s3.PutAreBrowserAcceleratorKeysEnabled(v.debug)
	}
	if s4 := query[webview2.ICoreWebView2Settings4](unsafe.Pointer(s), iidSettings4); s4 != nil {
		s4.PutIsPasswordAutosaveEnabled(false)
		s4.PutIsGeneralAutofillEnabled(false)
	}
	if s6 := query[webview2.ICoreWebView2Settings6](unsafe.Pointer(s), iidSettings6); s6 != nil {
		s6.PutIsSwipeNavigationEnabled(false)
	}
}

const (
	iidController2 = "{c979903e-d4ca-4228-92eb-47ee3fa96eab}"
	iidSettings3   = "{fdb5ab74-af33-4854-84f0-0a631deb5eba}"
	iidSettings4   = "{cb56846c-4168-4d53-b04f-03b6d6796ff2}"
	iidSettings6   = "{11cb3acd-9bc8-43b8-83bf-f40753714f87}"
)

// query asks a COM object for another of its interfaces; nil when it has
// none, as on an older WebView2.
func query[T any](obj unsafe.Pointer, iid string) *T {
	var out *T
	unk := (*webview2.IUnknown)(obj)
	hr, _, _ := unk.Vtbl.QueryInterface.Call(uintptr(obj), uintptr(unsafe.Pointer(webview2.NewGUID(iid))), uintptr(unsafe.Pointer(&out)))
	if hr != 0 {
		return nil
	}
	return out
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		unk := (*webview2.IUnknown)(obj)
		unk.Vtbl.Release.Call(uintptr(obj))
	}
}

func (v *webView) resize() {
	if v.ctl == nil {
		return
	}
	r := clientRect(v.a.hwnd)
	v.ctl.PutBounds(webview2.RECT{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom})
}

func (v *webView) moved() {
	if v.ctl != nil {
		v.ctl.NotifyParentWindowPositionChanged()
	}
}

func (v *webView) focus() {
	if v.ctl != nil {
		v.ctl.MoveFocus(webview2.COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC)
	}
}

func (v *webView) post(s string) {
	if v.wv != nil {
		if err := v.wv.PostWebMessageAsString(s); err != nil {
			log.Print("post: ", err)
		}
	}
}

func (v *webView) devTools() {
	if v.debug && v.wv != nil {
		v.wv.OpenDevToolsWindow()
	}
}

// WebMessageReceived takes the page's message to the backend.
func (v *webView) WebMessageReceived(_ *webview2.ICoreWebView2, args *webview2.ICoreWebView2WebMessageReceivedEventArgs) uintptr {
	s, err := args.TryGetWebMessageAsString()
	if err == nil && s != "" {
		v.a.backend.Receive([]byte(s))
	}
	return 0
}

// WebResourceRequested answers the page's own address with its files.
func (v *webView) WebResourceRequested(_ *webview2.ICoreWebView2, args *webview2.ICoreWebView2WebResourceRequestedEventArgs) uintptr {
	req, err := args.GetRequest()
	if err != nil || req == nil {
		return 0
	}
	defer release(unsafe.Pointer(req))
	uri, _ := req.GetUri()
	status, reason, headers := 404, "Not Found", "Content-Type: text/plain"
	var body []byte
	if u, err := url.Parse(uri); err == nil && "https://"+u.Host == web.Origin {
		if b, typ, ok := web.File(u.Path); ok {
			status, reason, body = 200, "OK", b
			headers = "Content-Type: " + typ + "\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff"
		}
	}
	stream := memStream(body)
	resp, err := v.env.CreateWebResourceResponse(stream, status, reason, headers)
	release(unsafe.Pointer(stream))
	if err != nil || resp == nil {
		log.Print("response: ", err)
		return 0
	}
	args.PutResponse(resp)
	release(unsafe.Pointer(resp))
	return 0
}

// NavigationStarting keeps the window on the page: an address elsewhere
// opens in the browser instead.
func (v *webView) NavigationStarting(_ *webview2.ICoreWebView2, args *webview2.ICoreWebView2NavigationStartingEventArgs) uintptr {
	uri, _ := args.GetUri()
	if len(uri) >= len(web.Origin) && uri[:len(web.Origin)] == web.Origin {
		return 0
	}
	args.PutCancel(true)
	openURL(uri)
	return 0
}

// NewWindowRequested opens what would be a new window in the browser.
func (v *webView) NewWindowRequested(_ *webview2.ICoreWebView2, args *webview2.ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	uri, _ := args.GetUri()
	args.PutHandled(true)
	openURL(uri)
	return 0
}

// PermissionRequested lets the page read the clipboard, for Paste in the
// terminal's menu, and nothing else: no camera, microphone or place.
func (v *webView) PermissionRequested(_ *webview2.ICoreWebView2, args *webview2.ICoreWebView2PermissionRequestedEventArgs) uintptr {
	kind, _ := args.GetPermissionKind()
	if kind == webview2.COREWEBVIEW2_PERMISSION_KIND_CLIPBOARD_READ {
		args.PutState(webview2.COREWEBVIEW2_PERMISSION_STATE_ALLOW)
	} else {
		args.PutState(webview2.COREWEBVIEW2_PERMISSION_STATE_DENY)
	}
	return 0
}

// ProcessFailed loads the page again after its renderer died; the agents
// never noticed, since they run in Linux.
func (v *webView) ProcessFailed(_ *webview2.ICoreWebView2, args *webview2.ICoreWebView2ProcessFailedEventArgs) uintptr {
	kind, _ := args.GetProcessFailedKind()
	log.Printf("WebView2 process failed: kind %d", kind)
	switch kind {
	case webview2.COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED:
		messageBox(v.a.hwnd, "Irie", "WebView2 stopped. Open Irie again; your agents kept running.", mbIconWarning)
		pPostMessageW.Call(uintptr(v.a.hwnd), wmClose, 0, 0)
	case webview2.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED, webview2.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_UNRESPONSIVE:
		v.wv.Reload()
	}
	return 0
}

// memStream is an IStream over a copy of b.
func memStream(b []byte) *webview2.IStream {
	var p uintptr
	if len(b) > 0 {
		p = uintptr(unsafe.Pointer(&b[0]))
	}
	s, _, _ := pSHCreateMemStream.Call(p, uintptr(len(b)))
	return ptr[webview2.IStream](s)
}

// ---- Snapshots -----------------------------------------------------------------

// capture is one CapturePreview: the stream it fills and where its PNG
// goes.
type capture struct {
	v       *webView
	stream  *webview2.IStream
	file    string
	then    func()
	handler *webview2.ICoreWebView2CapturePreviewCompletedHandler
}

func (c *capture) QueryInterface(refiid, object uintptr) uintptr { return 0x80004002 }
func (c *capture) AddRef() uintptr                               { return 1 }
func (c *capture) Release() uintptr                              { return 1 }

func (c *capture) CapturePreviewCompleted(code uintptr) uintptr {
	if int32(code) < 0 {
		log.Printf("snapshot %s: %#x", c.file, code)
		os.Exit(1)
	}
	b, err := streamBytes(c.stream)
	release(unsafe.Pointer(c.stream))
	if err == nil {
		err = os.WriteFile(c.file, b, 0o644)
	}
	if err != nil {
		log.Printf("snapshot %s: %v", c.file, err)
		os.Exit(1)
	}
	log.Printf("snapshot: %s, %d bytes", c.file, len(b))
	c.then()
	return 0
}

// shots are what --snapshot draws: the window as it opens, then with
// Settings open, then with a card's menu open.
var shots = []struct{ file, script string }{
	{"window.png", ""},
	{"settings.png", `document.dispatchEvent(new KeyboardEvent('keydown', {key: ',', ctrlKey: true, bubbles: true}));`},
	{"menu.png", `document.querySelector('.scrim') && document.querySelector('.scrim').remove(); const c = document.querySelector('.card.loud') || document.querySelector('.card'); if (c) c.dispatchEvent(new MouseEvent('contextmenu', {bubbles: true, clientX: 200, clientY: 260}));`},
}

// snapshots draws each shot into dir in turn, then quits.
func (v *webView) snapshots(dir string) {
	os.MkdirAll(dir, 0o755)
	var next func(i int)
	next = func(i int) {
		if i == len(shots) {
			pPostMessageW.Call(uintptr(v.a.hwnd), wmClose, 0, 0)
			return
		}
		if shots[i].script != "" {
			v.wv.ExecuteScript(shots[i].script, nil)
		}
		// The page draws what the script did in a frame or two.
		go func() {
			time.Sleep(900 * time.Millisecond)
			v.a.ui(func() { v.capture(filepath.Join(dir, shots[i].file), func() { next(i + 1) }) })
		}()
	}
	next(0)
}

func (v *webView) capture(file string, then func()) {
	c := &capture{v: v, stream: memStream(nil), file: file, then: then}
	c.handler = webview2.NewICoreWebView2CapturePreviewCompletedHandler(c)
	v.captures = append(v.captures, c)
	if err := v.wv.CapturePreview(webview2.COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG, c.stream, c.handler); err != nil {
		log.Printf("snapshot %s: %v", file, err)
		os.Exit(1)
	}
}

// iStreamVtbl is IStream's whole table, which the binding's leaves short.
type iStreamVtbl struct {
	QueryInterface, AddRef, Release                                     uintptr
	Read, Write, Seek, SetSize, CopyTo, Commit, Revert, Lock, Unlock, Stat, Clone uintptr
}

// streamBytes reads a stream from its start to its end.
func streamBytes(s *webview2.IStream) ([]byte, error) {
	vt := *(**iStreamVtbl)(unsafe.Pointer(s))
	this := uintptr(unsafe.Pointer(s))
	// Seek(0, STREAM_SEEK_SET): a LARGE_INTEGER goes in one register on 64-bit Windows.
	if hr, _, _ := syscall.SyscallN(vt.Seek, this, 0, 0, 0); hr != 0 {
		return nil, fmt.Errorf("IStream.Seek: %#x", hr)
	}
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		var n uint32
		hr, _, _ := syscall.SyscallN(vt.Read, this, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if int32(hr) < 0 {
			return nil, fmt.Errorf("IStream.Read: %#x", hr)
		}
		out = append(out, buf[:n]...)
		if n == 0 || hr == 1 { // S_FALSE: the end
			return out, nil
		}
	}
}
