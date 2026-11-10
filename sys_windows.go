//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	pMessageBox = user32.NewProc("MessageBoxW")
)

// newCmd starts a console program without flashing a black window.
func newCmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

func openFolder(p string) { exec.Command("explorer", p).Start() }

func msgBox(title, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	pMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10)
}

func defaultRoot() string { return `C:\OurPlaceCraftLauncher` }

func openURL(u string) { exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start() }
