//go:build windows

package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	advapi32              = syscall.NewLazyDLL("advapi32.dll")
	shell32               = syscall.NewLazyDLL("shell32.dll")
	ole32                 = syscall.NewLazyDLL("ole32.dll")
	pRegSetKeyValueW      = advapi32.NewProc("RegSetKeyValueW")
	pRegDeleteTreeW       = advapi32.NewProc("RegDeleteTreeW")
	pSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	pCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	pCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
	pCreateMutexW         = kernel32.NewProc("CreateMutexW")
	pOpenMutexW           = kernel32.NewProc("OpenMutexW")
	pCloseHandle          = kernel32.NewProc("CloseHandle")
	pFindWindowW          = user32.NewProc("FindWindowW")
	pShowWindow           = user32.NewProc("ShowWindow")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
)

const (
	hkcu         = 0x80000001
	uninstKey    = `Software\Microsoft\Windows\CurrentVersion\Uninstall\OurPlaceCraftLauncher`
	lnkName      = "Our Place Craft Launcher.lnk"
	appName      = "Our Place Craft Launcher (beta)"
	launcherLock = "OurPlaceCraftLauncher2"
)

func u16p(s string) uintptr { p, _ := syscall.UTF16PtrFromString(s); return uintptr(unsafe.Pointer(p)) }

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func runPowerShell(script string) error {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[i*2], b[i*2+1] = byte(v), byte(v>>8)
	}
	return newCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b)).Run()
}

func regSZ(name, val string) {
	s := syscall.StringToUTF16(val)
	pRegSetKeyValueW.Call(hkcu, u16p(uninstKey), u16p(name), 1, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)*2))
}

func regDW(name string, v uint32) {
	pRegSetKeyValueW.Call(hkcu, u16p(uninstKey), u16p(name), 4, uintptr(unsafe.Pointer(&v)), 4)
}

func init() {
	setupRegister = func(root, exe, ico string) {
		regSZ("DisplayName", appName)
		if ico != "" {
			regSZ("DisplayIcon", ico)
		} else {
			regSZ("DisplayIcon", exe+",0")
		}
		regSZ("DisplayVersion", "2.0 beta")
		regSZ("Publisher", "Our Place")
		regSZ("InstallLocation", root)
		regSZ("UninstallString", `"`+exe+`" --uninstall`)
		regDW("NoModify", 1)
		regDW("NoRepair", 1)
	}
	setupShortcuts = func(root, exe, ico string, desktop bool) error {
		iconLoc := exe + ",0"
		if ico != "" {
			iconLoc = ico + ",0"
		}
		defer refreshIcons()
		dirs := "[Environment]::GetFolderPath('Programs')"
		if desktop {
			dirs = "[Environment]::GetFolderPath('Desktop'), " + dirs
		}
		return runPowerShell(`$w = New-Object -ComObject WScript.Shell
foreach ($d in @(` + dirs + `)) {
  $s = $w.CreateShortcut((Join-Path $d ` + psQuote(lnkName) + `))
  $s.TargetPath = ` + psQuote(exe) + `
  $s.WorkingDirectory = ` + psQuote(root) + `
  $s.IconLocation = ` + psQuote(iconLoc) + `
  $s.Description = ` + psQuote(appName) + `
  $s.Save()
}`)
	}
	setupPickDir = func(title string) string {
		// the folder dialog needs a COM apartment on its own thread
		res := make(chan string, 1)
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			pCoInitializeEx.Call(0, 2)
			var bi struct {
				hwndOwner      uintptr
				pidlRoot       uintptr
				pszDisplayName *uint16
				lpszTitle      *uint16
				ulFlags        uint32
				lpfn           uintptr
				lParam         uintptr
				iImage         int32
			}
			name := make([]uint16, 260)
			bi.hwndOwner = mainHwnd
			bi.pszDisplayName = &name[0]
			bi.lpszTitle, _ = syscall.UTF16PtrFromString(title)
			bi.ulFlags = 0x0001 | 0x0040 // BIF_RETURNONLYFSDIRS | BIF_NEWDIALOGSTYLE
			pidl, _, _ := pSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
			if pidl == 0 {
				res <- ""
				return
			}
			defer pCoTaskMemFree.Call(pidl)
			buf := make([]uint16, 1024)
			pSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&buf[0])))
			res <- syscall.UTF16ToString(buf)
		}()
		return <-res
	}
	oldLauncherRunning = func() bool {
		h, _, _ := pOpenMutexW.Call(0x00100000, 0, u16p("OurPlaceCraftAdventureLauncher"))
		if h != 0 {
			pCloseHandle.Call(h)
			return true
		}
		return false
	}
	removeOldLauncher = func(old string) {
		runPowerShell(`foreach ($d in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('CommonDesktopDirectory'))) {
  Remove-Item -LiteralPath (Join-Path $d 'Our Place Craft - Adventure.lnk') -ErrorAction SilentlyContinue }`)
		pRegDeleteTreeW.Call(hkcu, u16p(`Software\Microsoft\Windows\CurrentVersion\Uninstall\OurPlaceCraftAdventure`))
		if os.RemoveAll(old) != nil { // something still holds a file: finish a bit later
			c := exec.Command("cmd.exe", "/C", `ping 127.0.0.1 -n 4 >nul & rmdir /S /Q "`+old+`"`)
			c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			c.Dir = os.TempDir()
			c.Start()
		}
	}
	restartLauncher = func(exe string) error {
		c := exec.Command(exe, "--after-update")
		c.Dir = filepath.Dir(exe)
		return c.Start()
	}
	setupStart = func(exe string) error {
		c := exec.Command(exe)
		c.Dir = filepath.Dir(exe)
		return c.Start()
	}
	documentsDir = func() string {
		out, err := newCmd("powershell.exe", "-NoProfile", "-Command", "[Console]::OutputEncoding=[Text.Encoding]::UTF8; [Environment]::GetFolderPath('MyDocuments')").Output()
		if p := strings.TrimSpace(string(out)); err == nil && p != "" {
			return p
		}
		return filepath.Join(os.Getenv("USERPROFILE"), "Documents")
	}
	launcherBusy = func() bool {
		h, _, _ := pOpenMutexW.Call(0x00100000, 0, u16p(launcherLock)) // SYNCHRONIZE
		if h != 0 {
			pCloseHandle.Call(h)
			return true
		}
		return false
	}
}

var mainHwnd uintptr

// refreshIcons asks Explorer to drop its cached icons so the new ones show right away.
func refreshIcons() {
	shell32.NewProc("SHChangeNotify").Call(0x08000000, 0, 0, 0) // SHCNE_ASSOCCHANGED
	if c := newCmd(filepath.Join(os.Getenv("SystemRoot"), "System32", "ie4uinit.exe"), "-show"); c != nil {
		c.Run()
	}
}

// singleInstance returns false (after bringing the open window forward) if the launcher already runs.
// With wait (right after a self-update) it waits for the previous launcher to exit.
func singleInstance(title string, wait bool) bool {
	for i := 0; wait && i < 60; i++ {
		h, _, _ := pCreateMutexW.Call(0, 0, u16p(launcherLock))
		if syscall.GetLastError() != syscall.Errno(183) {
			return true
		}
		pCloseHandle.Call(h)
		time.Sleep(250 * time.Millisecond)
	}
	pCreateMutexW.Call(0, 0, u16p(launcherLock))
	if syscall.GetLastError() == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		if w, _, _ := pFindWindowW.Call(0, u16p(title)); w != 0 {
			pShowWindow.Call(w, 9)
			pSetForegroundWindow.Call(w)
		}
		return false
	}
	return true
}

func msgYesNo(title, text string) bool {
	r, _, _ := pMessageBox.Call(0, u16p(text), u16p(title), 0x4|0x20) // MB_YESNO | MB_ICONQUESTION
	return r == 6
}

func msgInfo(title, text string) { pMessageBox.Call(0, u16p(text), u16p(title), 0x40) }

// uninstall removes the launcher (and, if the player agrees, all instances and worlds).
func uninstall(root string) {
	if launcherBusy() {
		msgBox(appName, "Закройте лаунчер Our Place Craft и запустите удаление ещё раз.")
		return
	}
	if !msgYesNo(appName, "Удалить Our Place Craft Launcher с этого компьютера?") {
		return
	}
	all := msgYesNo(appName, "Удалить также все сборки, миры, скриншоты и скачанные файлы?\n\nПапка: "+root+
		"\n\n«Да» — удалить всё.\n«Нет» — удалить только лаунчер, а сборки и миры оставить.")
	runPowerShell(`foreach ($d in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  Remove-Item -LiteralPath (Join-Path $d ` + psQuote(lnkName) + `) -ErrorAction SilentlyContinue }`)
	pRegDeleteTreeW.Call(hkcu, u16p(uninstKey))
	target := `del /F /Q "` + filepath.Join(root, installExe) + `"`
	if all {
		target = `rmdir /S /Q "` + root + `"`
	}
	c := exec.Command("cmd.exe", "/C", `ping 127.0.0.1 -n 3 >nul & `+target)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	c.Dir = os.TempDir()
	c.Start()
	msgInfo(appName, "Our Place Craft Launcher удалён.")
}
