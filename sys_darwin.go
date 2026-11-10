//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func init() {
	ruleOSName, javaGUIName, javaCLIName, classpathSep = "osx", "java", "java", ":"
}

func newCmd(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }
func openFolder(p string)                          { exec.Command("open", p).Start() }
func openURL(u string)                             { exec.Command("open", u).Start() }

func msgBox(title, text string) {
	q := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) }
	exec.Command("osascript", "-e", `display dialog "`+q(text)+`" with title "`+q(title)+`" buttons {"OK"} default button "OK"`).Run()
}

func defaultRoot() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Application Support", "OurPlaceCraftLauncher")
}

func totalRAMGB() int {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	b, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return int((b + (1 << 29)) >> 30)
}

func init() {
	updateAsset, updateManifest, updateExeSuffix = "OurPlaceCraftLauncher-macOS-update.zip", "launcher-mac.json", "/OurPlaceCraftLauncher"
	// the binary sits in Our Place Craft Launcher.app/Contents/MacOS: reopen the whole app
	restartLauncher = func(exe string) error {
		app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if !strings.HasSuffix(app, ".app") {
			return exec.Command(exe).Start()
		}
		return exec.Command("/bin/sh", "-c", `sleep 1; open -n "$0"`, app).Start()
	}
}
