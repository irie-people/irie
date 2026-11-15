// Command windows-app is Irie for Windows, Irie.exe (docs/WINDOWS.md): a
// window that shows your agents as the Mac app does, a sidebar of session
// cards and the shown session's terminal, for Irie running inside WSL 2.
// The window draws a web page (internal/winapp/web) in WebView2, and
// internal/winapp carries what the page says to Irie in Linux through
// one wsl.exe running `irie bridge`, and what Irie says back.
//
// It is built for Windows only:
//
//	GOOS=windows go build -ldflags "-H windowsgui" -o Irie.exe ./cmd/windows-app
//
// Irie.exe --snapshot DIR draws the window into DIR as PNGs, once its
// sessions show, and quits: for CI on Windows, with IRIE_APP_BRIDGE
// naming a stand-in for the bridge (tools/winpreview -bridge).
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/irie-people/irie/internal/brand"
)

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, brand.Exe+".exe is "+brand.Name+" for Windows; on a Mac, "+brand.Name+".app is its own, and Linux has the terminal sidebar.")
		os.Exit(2)
	}
	os.Exit(run(os.Args[1:]))
}
