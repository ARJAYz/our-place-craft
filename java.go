package main

// Java runtimes: every Minecraft version names the Java it needs (8, 16, 17, 21, 25…);
// the launcher keeps one runtime per major version in <root>/runtimes/java-<N>.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var adoptiumAPI = "https://api.adoptium.net/v3/assets/latest/"

// javaMajorFor maps what a version asks for to a runtime that exists as a JRE build.
func javaMajorFor(want int) int {
	switch {
	case want <= 8:
		return 8
	case want <= 17:
		return 17 // 16 is end of life; 17 runs everything that asked for 16
	default:
		return want
	}
}

func runtimeDir(root string, major int) string {
	return filepath.Join(root, "runtimes", "java-"+strconv.Itoa(major))
}

// findJava looks for bin/<name> in a runtime folder (the archive keeps its top folder).
func findJava(dir, name string) string {
	if p := filepath.Join(dir, "bin", name); fileExists(p) {
		return p
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*", "bin", name)); len(m) > 0 {
		return m[0]
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*", "Contents", "Home", "bin", name)); len(m) > 0 { // macOS JRE
		return m[0]
	}
	return ""
}

// javaFor returns the java executable for the given major version, or "" if not installed.
// legacyRoot is the folder of the old single-pack install (its Java 21 lives in <root>/runtime).
func javaFor(root string, major int, name string, legacyRoot string) string {
	major = javaMajorFor(major)
	if j := findJava(runtimeDir(root, major), name); j != "" {
		return j
	}
	if legacyRoot != "" && major == 21 {
		return findJava(filepath.Join(legacyRoot, "runtime"), name)
	}
	return ""
}

func ensureJavaMajor(root string, major int, p *Progress) error {
	major = javaMajorFor(major)
	if findJava(runtimeDir(root, major), javaGUIName) != "" {
		return nil
	}
	ext := ".zip"
	if ruleOSName != "windows" {
		ext = ".tar.gz"
	}
	adoptOS, arch := ruleOSName, "x64"
	if adoptOS == "osx" {
		adoptOS = "mac"
	}
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	var assets []struct {
		Binary struct {
			Package struct {
				Link string `json:"link"`
				Size int64  `json:"size"`
				Name string `json:"name"`
			} `json:"package"`
		} `json:"binary"`
	}
	url := fmt.Sprintf("%s%d/hotspot?architecture=%s&image_type=jre&vendor=eclipse&os=%s", adoptiumAPI, major, arch, adoptOS)
	if err := getJSON(url, &assets); err != nil {
		return fmt.Errorf("не удалось найти Java %d: %w", major, err)
	}
	var link string
	var size int64
	for _, a := range assets {
		if strings.HasSuffix(a.Binary.Package.Name, ext) {
			link, size = a.Binary.Package.Link, a.Binary.Package.Size
			break
		}
	}
	if link == "" {
		return fmt.Errorf("для этой системы нет сборки Java %d", major)
	}
	arc := filepath.Join(root, "downloads", "java"+strconv.Itoa(major)+ext)
	if err := downloadAll([]dl{{URL: link, Dest: arc, Size: size}}, p, fmt.Sprintf("Скачиваем Java %d", major)); err != nil {
		return err
	}
	p.set(fmt.Sprintf("Распаковываем Java %d", major), 0)
	dir := runtimeDir(root, major)
	os.RemoveAll(dir)
	var err error
	if ext == ".zip" {
		err = unzip(arc, dir)
	} else {
		err = untarGz(arc, dir)
	}
	os.Remove(arc)
	if err != nil {
		return fmt.Errorf("архив Java повреждён: %w", err)
	}
	if findJava(dir, javaGUIName) == "" {
		return errors.New("Java распакована, но " + javaGUIName + " не найден")
	}
	return nil
}
