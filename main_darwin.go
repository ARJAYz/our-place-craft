//go:build darwin

package main

import (
	"os"

	webview "github.com/webview/webview_go"
)

const launcherTitle = "Our Place Craft Launcher (beta)"

// macOS: the .app runs the launcher itself (no installer); data lives in
// ~/Library/Application Support/OurPlaceCraftLauncher. Updates: a new .app from GitHub.
func main() {
	root := defaultRoot()
	os.MkdirAll(root, 0o755)
	a := newApp(root)
	if self, err := os.Executable(); err == nil {
		go cleanupOldExe(self)
	}
	go a.updateLoop()
	url, err := serve(a, "127.0.0.1:0")
	if err != nil {
		msgBox("Our Place Craft", "Не удалось запустить: "+err.Error())
		return
	}
	wv := webview.New(false)
	if wv == nil {
		msgBox("Our Place Craft", "Не удалось открыть окно лаунчера.")
		os.Exit(1)
	}
	defer wv.Destroy()
	wv.SetTitle(launcherTitle)
	wv.SetSize(1180, 780, webview.HintNone)
	wv.SetSize(980, 640, webview.HintMin)
	a.quit = func() { wv.Dispatch(wv.Terminate) }
	wv.Navigate(url)
	wv.Run()
}
