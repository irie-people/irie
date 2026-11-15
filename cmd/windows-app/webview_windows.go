package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/webviewloader"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/winapp/web"
)

// webView is the window's WebView2: Microsoft's Edge drawing the page,
// loaded from the app itself (package web) and never from the network,
// whose messages go to the backend and back (docs/WINDOWS.md §5). Every
// call is on the window's thread.
type webView struct {
	a     *app
	env   com
	ctl   com
	wv    com
	err   error
	debug bool
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
	for v.wv == 0 && v.err == nil {
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
	v.env = com(uintptr(unsafe.Pointer(env)))
	// The loader lets its own reference go once this returns.
	v.env.addRef()
	done := newHandler(func(code, ctl uintptr) uintptr {
		v.controllerDone(code, com(ctl))
		return 0
	})
	if err := failed(v.env.call(envCreateController, uintptr(v.a.hwnd), done)); err != nil {
		v.err = fmt.Errorf("WebView2's controller: %v", err)
	}
	return 0
}

func (v *webView) controllerDone(code uintptr, ctl com) {
	if int32(code) < 0 || ctl == 0 {
		v.err = fmt.Errorf("WebView2's controller failed: %#x", uint32(code))
		return
	}
	ctl.addRef()
	wv := ctl.out(ctlGetCoreWebView2)
	if wv == 0 {
		v.err = fmt.Errorf("WebView2 gave no web view")
		return
	}
	v.settings(wv)
	// The ground the page draws on, so nothing flashes white as it loads:
	// COREWEBVIEW2_COLOR's A, R, G and B in a word.
	if c2 := ctl.query(iidController2); c2 != 0 {
		g := uintptr(255) | 0xE9<<8 | 0xEB<<16 | 0xDF<<24
		if windowsDark() {
			g = uintptr(255) | 0x0E<<8 | 0x14<<16 | 0x10<<24
		}
		c2.call(ctl2PutBackground, g)
		c2.release()
	}
	wv.on(wvAddMessage, v.message)
	wv.withString(wvAddResourceFilter, web.Origin+"/*", resourceContextAll)
	wv.on(wvAddResource, v.resource)
	wv.on(wvAddNavStarting, v.navigating)
	wv.on(wvAddNewWindow, v.newWindow)
	wv.on(wvAddPermission, v.permission)
	wv.on(wvAddProcessFailed, v.processFailed)
	v.ctl = ctl
	v.resize()
	ctl.call(ctlPutIsVisible, 1)
	if err := failed(wv.withString(wvNavigate, web.Origin+"/index.html")); err != nil {
		v.err = fmt.Errorf("WebView2 did not load the page: %v", err)
		return
	}
	v.wv = wv
	v.focus()
}

// settings keep the page an app's: no browser keys, menus, zoom, status
// bar, autofill or swipes, and no developer tools unless IRIE_APP_DEBUG=1.
func (v *webView) settings(wv com) {
	s := wv.out(wvGetSettings)
	if s == 0 {
		log.Print("WebView2 gave no settings")
		return
	}
	defer s.release()
	s.call(setPutStatusBar, 0)
	s.call(setPutContextMenus, boolArg(v.debug))
	s.call(setPutDevTools, boolArg(v.debug))
	s.call(setPutZoomControl, 0)
	s.call(setPutErrorPage, 1)
	s.call(setPutHostObjects, 0)
	s.call(setPutScriptDialogs, 0)
	if s3 := s.query(iidSettings3); s3 != 0 {
		s3.call(set3PutBrowserKeys, boolArg(v.debug))
		s3.release()
	}
	if s4 := s.query(iidSettings4); s4 != 0 {
		s4.call(set4PutPasswordSave, 0)
		s4.call(set4PutAutofill, 0)
		s4.release()
	}
	if s6 := s.query(iidSettings6); s6 != 0 {
		s6.call(set6PutSwipe, 0)
		s6.release()
	}
}

func (v *webView) resize() {
	if v.ctl != 0 {
		putBounds(v.ctl, clientRect(v.a.hwnd))
	}
}

func (v *webView) moved() {
	if v.ctl != 0 {
		v.ctl.call(ctlNotifyMoved)
	}
}

func (v *webView) focus() {
	if v.ctl != 0 {
		v.ctl.call(ctlMoveFocus, focusProgrammatic)
	}
}

func (v *webView) post(s string) {
	if v.wv != 0 {
		if err := failed(v.wv.withString(wvPostString, s)); err != nil {
			log.Print("post: ", err)
		}
	}
}

func (v *webView) devTools() {
	if v.debug && v.wv != 0 {
		v.wv.call(wvOpenDevTools)
	}
}

// message takes the page's message to the backend.
func (v *webView) message(_, args com) {
	if s := args.str(webMessageTryAsString); s != "" {
		v.a.backend.Receive([]byte(s))
	}
}

// resource answers the page's own address with its files.
func (v *webView) resource(_, args com) {
	req := args.out(resArgsGetRequest)
	if req == 0 {
		return
	}
	uri := req.str(requestGetURI)
	req.release()
	status, reason, headers := 404, "Not Found", "Content-Type: text/plain"
	var body []byte
	if u, err := url.Parse(uri); err == nil && "https://"+u.Host == web.Origin {
		if b, typ, ok := web.File(u.Path); ok {
			status, reason, body = 200, "OK", b
			headers = "Content-Type: " + typ + "\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff"
		}
	}
	stream := memStream(body)
	defer stream.release()
	reasonP, _ := syscall.UTF16PtrFromString(reason)
	headersP, _ := syscall.UTF16PtrFromString(headers)
	var resp com
	hr := v.env.call(envCreateResponse, uintptr(stream), uintptr(status), uintptr(unsafe.Pointer(reasonP)), uintptr(unsafe.Pointer(headersP)), uintptr(unsafe.Pointer(&resp)))
	if err := failed(hr); err != nil || resp == 0 {
		log.Printf("response to %s: %v", uri, err)
		return
	}
	args.call(resArgsPutResponse, uintptr(resp))
	resp.release()
}

// navigating keeps the window on the page: an address elsewhere opens in
// the browser instead.
func (v *webView) navigating(_, args com) {
	uri := args.str(navArgsGetURI)
	if strings.HasPrefix(uri, web.Origin+"/") {
		return
	}
	args.call(navArgsPutCancel, 1)
	openURL(uri)
}

// newWindow opens what would be a new window in the browser.
func (v *webView) newWindow(_, args com) {
	uri := args.str(newWinArgsGetURI)
	args.call(newWinArgsPutHandled, 1)
	openURL(uri)
}

// permission lets the page read the clipboard, for Paste in the
// terminal's menu, and nothing else: no camera, microphone or place.
func (v *webView) permission(_, args com) {
	var kind int32
	args.call(permArgsGetKind, uintptr(unsafe.Pointer(&kind)))
	state := uintptr(permissionDeny)
	if kind == permissionClipRead {
		state = permissionAllow
	}
	args.call(permArgsPutState, state)
}

// processFailed loads the page again after its renderer died; the agents
// never noticed, since they run in Linux.
func (v *webView) processFailed(_, args com) {
	var kind int32
	args.call(failArgsGetKind, uintptr(unsafe.Pointer(&kind)))
	log.Printf("WebView2 process failed: kind %d", kind)
	switch kind {
	case failedBrowserExited:
		v.a.problem("WebView2 stopped. Open " + brand.Name + " again; your agents kept running.")
		pPostMessageW.Call(uintptr(v.a.hwnd), wmClose, 0, 0)
	case failedRenderExited, failedUnresponsive:
		v.wv.call(wvReload)
	}
}

// memStream is an IStream over a copy of b.
func memStream(b []byte) com {
	var p uintptr
	if len(b) > 0 {
		p = uintptr(unsafe.Pointer(&b[0]))
	}
	s, _, _ := pSHCreateMemStream.Call(p, uintptr(len(b)))
	return com(s)
}

// ---- Snapshots ---------------------------------------------------------------------

// shots are what --snapshot draws: the window as it opens, then with
// Settings open, then with a card's menu open.
var shots = []struct{ file, script string }{
	{"window.png", ""},
	{"settings.png", `document.dispatchEvent(new KeyboardEvent('keydown', {key: ',', ctrlKey: true, bubbles: true}));`},
	{"menu.png", `document.querySelectorAll('.scrim').forEach((s) => s.remove()); const c = document.querySelector('.card.loud') || document.querySelector('.card'); if (c) c.dispatchEvent(new MouseEvent('contextmenu', {bubbles: true, clientX: 200, clientY: 260}));`},
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
			v.wv.withString(wvExecuteScript, shots[i].script, 0)
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
	stream := memStream(nil)
	done := newHandler(func(code, _ uintptr) uintptr {
		defer stream.release()
		if err := failed(code); err != nil {
			log.Printf("snapshot %s: %v", file, err)
			os.Exit(1)
		}
		b, err := streamBytes(stream)
		if err == nil {
			err = os.WriteFile(file, b, 0o644)
		}
		if err != nil {
			log.Printf("snapshot %s: %v", file, err)
			os.Exit(1)
		}
		log.Printf("snapshot: %s, %d bytes", file, len(b))
		then()
		return 0
	})
	if err := failed(v.wv.call(wvCapturePreview, captureFormatPNG, uintptr(stream), done)); err != nil {
		log.Printf("snapshot %s: %v", file, err)
		os.Exit(1)
	}
}

// IStream's methods, after IUnknown's: Read, Write, Seek, …
const (
	streamRead = 3
	streamSeek = 5
)

// streamBytes reads a stream from its start to its end.
func streamBytes(s com) ([]byte, error) {
	// Seek(0, STREAM_SEEK_SET, NULL): a LARGE_INTEGER goes in one register on 64-bit Windows.
	if err := failed(s.call(streamSeek, 0, 0, 0)); err != nil {
		return nil, fmt.Errorf("IStream.Seek: %v", err)
	}
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		var n uint32
		hr := s.call(streamRead, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if err := failed(hr); err != nil {
			return nil, fmt.Errorf("IStream.Read: %v", err)
		}
		out = append(out, buf[:n]...)
		if n == 0 || hr == 1 { // S_FALSE: the end
			return out, nil
		}
	}
}
