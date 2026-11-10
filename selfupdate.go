package main

// Launcher self-update. The public build lives in the branch "launcher" of ARJAYz/our-place-craft:
//   launcher.json            {"version":"20261030-1530","sha256":"…","size":123,"notes":"…"}
//   OurPlaceCraftLauncher-Setup.zip   (zip with OurPlaceCraftLauncher-Setup.exe; started directly the exe acts as the installer)
// The running exe cannot be overwritten on Windows but it can be renamed: the launcher renames
// itself to .old, puts the new exe in place, starts it and exits; the new one deletes the .old.

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// set at build time: -ldflags "-X main.launcherVersion=20261030-1530"
var launcherVersion = "dev"

var launcherUpdateBase = "https://raw.githubusercontent.com/ARJAYz/our-place-craft/launcher/"

// Platform settings (macOS overrides them in sys_darwin.go).
var (
	updateAsset     = "OurPlaceCraftLauncher-Setup.zip"
	updateManifest  = "launcher.json"
	updateExeSuffix = ".exe"
)

type UpdateInfo struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Notes   string `json:"notes,omitempty"`
}

// platform hook: start the freshly installed exe (setup_windows.go)
var restartLauncher = func(exe string) error { return nil }

func newerVersion(remote, local string) bool {
	return local != "dev" && len(remote) == len(local) && remote > local
}

// checkUpdate asks GitHub for the newest launcher; quietly does nothing when offline.
func (a *App) checkUpdate() *UpdateInfo {
	var u UpdateInfo
	if err := getJSON(launcherUpdateBase+updateManifest, &u); err != nil || u.SHA256 == "" || !newerVersion(u.Version, launcherVersion) {
		return nil
	}
	a.mu.Lock()
	a.update = &u
	a.mu.Unlock()
	return &u
}

// updateLoop checks at start and then every few hours.
func (a *App) updateLoop() {
	for {
		a.checkUpdate()
		time.Sleep(6 * time.Hour)
	}
}

// CheckUpdate (API)
func (a *App) CheckUpdate() *UpdateInfo { return a.checkUpdate() }

// SelfUpdate (API) downloads the new launcher, swaps the exe and restarts.
func (a *App) SelfUpdate() int {
	return a.async(func() (any, error) {
		a.mu.Lock()
		u, running := a.update, a.running
		a.mu.Unlock()
		if u == nil {
			return nil, errors.New("обновлений нет")
		}
		if running {
			return nil, errors.New("закройте игру, потом обновите лаунчер")
		}
		if !a.claim("update", "Обновляем лаунчер…") {
			return nil, errors.New("подождите, идёт установка")
		}
		self, err := os.Executable()
		if err == nil {
			err = a.installUpdate(self, u)
		}
		if err != nil {
			a.finish("Обновление не удалось: "+err.Error(), true)
			return nil, err
		}
		a.finish("Перезапускаем лаунчер…", false)
		if err := restartLauncher(self); err != nil {
			return nil, errors.New("лаунчер обновлён — откройте его заново")
		}
		go func() { time.Sleep(300 * time.Millisecond); a.quit() }()
		return true, nil
	})
}

func (a *App) installUpdate(self string, u *UpdateInfo) error {
	tmp := self + ".new"
	zp := self + ".zip"
	os.Remove(tmp)
	os.Remove(zp)
	defer os.Remove(zp)
	a.prog.set("Скачиваем новую версию лаунчера", u.Size)
	if err := download(dl{URL: launcherUpdateBase + updateAsset, Dest: zp, Size: u.Size}, a.prog.add); err != nil {
		return err
	}
	if sum, _ := fileSHA256(zp); sum != u.SHA256 {
		return errors.New("файл повреждён при скачивании, попробуйте ещё раз")
	}
	if err := extractExe(zp, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	old := self + ".old"
	os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		os.Rename(old, self)
		return err
	}
	return nil
}

// extractExe takes the single .exe out of the update zip.
func extractExe(zp, dst string) error {
	zr, err := zip.OpenReader(zp)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), strings.ToLower(updateExeSuffix)) || strings.HasSuffix(f.Name, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chmod(dst, 0o755)
	}
	return errors.New("в обновлении нет exe")
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// cleanupOldExe removes the previous exe left by an update (it may still be exiting).
func cleanupOldExe(self string) {
	old := self + ".old"
	for i := 0; i < 20 && fileExists(old); i++ {
		if os.Remove(old) == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
