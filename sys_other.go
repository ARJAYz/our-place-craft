//go:build !windows && !darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func init() {
	ruleOSName, javaGUIName, javaCLIName, classpathSep = "linux", "java", "java", ":"
}

func newCmd(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }
func openFolder(p string)                          { exec.Command("xdg-open", p).Start() }
func msgBox(title, text string)                    { fmt.Fprintln(os.Stderr, title+": "+text) }
func defaultRoot() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "OurPlaceCraftLauncher")
}

func openURL(u string) { exec.Command("xdg-open", u).Start() }
