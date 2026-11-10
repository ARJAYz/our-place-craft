//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	pGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	pGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
)

func totalRAMGB() int {
	var m struct {
		Length, MemoryLoad                         uint32
		TotalPhys, AvailPhys, TotalPage, AvailPage uint64
		TotalVirtual, AvailVirtual, AvailExtended  uint64
	}
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := pGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return 0
	}
	return int((m.TotalPhys + (1 << 29)) >> 30)
}

const (
	launcherTitle = "Our Place Craft Launcher (beta)"
	setupTitle    = "Установка — Our Place Craft Launcher (beta)"
)

func main() {
	self, _ := os.Executable()
	dir := filepath.Dir(self)
	if len(os.Args) > 1 && os.Args[1] == "--uninstall" {
		uninstall(dir)
		return
	}
	stampedCode = readStampFile(self)
	// The installed copy is <root>\OurPlaceCraftLauncher.exe; anything else is the installer.
	setup := !strings.EqualFold(filepath.Base(self), installExe)

	root, title, w, h := dir, launcherTitle, 1280, 820
	if setup {
		root, title, w, h = defaultRoot(), setupTitle, 1000, 820
	} else if !singleInstance(launcherTitle, len(os.Args) > 1 && os.Args[1] == "--after-update") {
		return
	}
	var a *App
	if setup {
		a = newSetupApp(root)
	} else {
		a = newApp(root)
		go cleanupOldExe(self)
		go a.updateLoop()
	}
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	a.screenW, a.screenH = int(sw), int(sh)
	url, err := serve(a, "127.0.0.1:0")
	if err != nil {
		msgBox("Our Place Craft", "Не удалось запустить: "+err.Error())
		return
	}
	data := filepath.Join(os.TempDir(), "OurPlaceCraftLauncher-webview")
	if !setup {
		data = filepath.Join(root, "webview")
	}
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus:     true,
		DataPath:      data,
		WindowOptions: webview2.WindowOptions{Title: title, Width: uint(w), Height: uint(h), IconId: 1, Center: true},
	})
	if wv == nil {
		msgBox("Our Place Craft", "Для лаунчера нужен компонент Microsoft Edge WebView2.\nОн есть в Windows 10/11; если его нет — установите «WebView2 Runtime» с сайта Microsoft.")
		os.Exit(1)
	}
	defer wv.Destroy()
	mainHwnd = uintptr(wv.Window())
	makeFrameless(wv, !setup)
	if !setup {
		wv.SetSize(980, 640, webview2.HintMin)
	}
	a.quit = func() { wv.Dispatch(wv.Terminate) }
	wv.Navigate(url)
	wv.Run()
}
