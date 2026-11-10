//go:build windows

package main

import (
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

// Our own window frame: the Windows caption is removed (the page draws the title bar and the
// window buttons) but WS_THICKFRAME/WS_CAPTION stay, so snapping, the shadow, the rounded
// corners on Windows 11 and the minimize/maximize animations keep working.

var (
	dwmapi             = syscall.NewLazyDLL("dwmapi.dll")
	pDwmExtendFrame    = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	pSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW   = user32.NewProc("CallWindowProcW")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pIsZoomed          = user32.NewProc("IsZoomed")
	pReleaseCapture    = user32.NewProc("ReleaseCapture")
	pPostMessageW      = user32.NewProc("PostMessageW")
	pMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	oldWndProc         uintptr
	frameProc          = syscall.NewCallback(framelessProc)
)

const (
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmNCCalcSize    = 0x0083
	wmNCLButtonDown = 0x00A1
	htCaption       = 2
)

type rect struct{ L, T, R, B int32 }

func framelessProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == wmNCCalcSize && wp != 0 {
		if z, _, _ := pIsZoomed.Call(hwnd); z != 0 {
			// maximized: keep the page inside the work area (a captionless window would spill past the screen edges)
			var mi struct {
				Size          uint32
				Monitor, Work rect
				Flags         uint32
			}
			mi.Size = uint32(unsafe.Sizeof(mi))
			if m, _, _ := pMonitorFromWindow.Call(hwnd, 2); m != 0 {
				if ok, _, _ := pGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi))); ok != 0 {
					**(**rect)(unsafe.Pointer(&lp)) = mi.Work
				}
			}
		}
		return 0 // the whole window is client area
	}
	r, _, _ := pCallWindowProcW.Call(oldWndProc, hwnd, msg, wp, lp)
	return r
}

func makeFrameless(wv webview2.WebView, resizable bool) {
	hwnd := uintptr(wv.Window())
	gwlpWndProc := -4
	oldWndProc, _, _ = pSetWindowLongPtrW.Call(hwnd, uintptr(gwlpWndProc), frameProc)
	if !resizable {
		gwlStyle := -16
		style := uintptr(0x00C00000 | 0x00080000 | 0x00020000 | 0x00040000 | 0x10000000) // CAPTION|SYSMENU|MINIMIZEBOX|THICKFRAME|VISIBLE
		pSetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle), style)
	}
	margins := [4]int32{0, 0, 0, 1} // keeps the DWM shadow
	pDwmExtendFrame.Call(hwnd, uintptr(unsafe.Pointer(&margins)))
	pSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0001|0x0002|0x0004|0x0020) // NOSIZE|NOMOVE|NOZORDER|FRAMECHANGED
	pPostMessageW.Call(hwnd, wmSize, 0, 0)

	// window.opcWin(op, arg) — called from the page; runs on the UI thread.
	wv.Bind("opcWin", func(op string, arg int) bool {
		switch op {
		case "drag", "resize":
			ht := uintptr(htCaption)
			if op == "resize" {
				if !resizable || arg < 10 || arg > 17 {
					return false
				}
				ht = uintptr(arg)
			}
			pReleaseCapture.Call()
			pPostMessageW.Call(hwnd, wmNCLButtonDown, ht, 0)
		case "min":
			pShowWindow.Call(hwnd, 6)
		case "max":
			if !resizable {
				return false
			}
			if z, _, _ := pIsZoomed.Call(hwnd); z != 0 {
				pShowWindow.Call(hwnd, 9)
			} else {
				pShowWindow.Call(hwnd, 3)
			}
		case "close":
			pPostMessageW.Call(hwnd, wmClose, 0, 0)
		}
		z, _, _ := pIsZoomed.Call(hwnd)
		return z != 0
	})
}
