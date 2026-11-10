package main

// Building the Java command line for any version (modern "arguments" and old "minecraftArguments").

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type LaunchOpts struct {
	Root, GameDir string
	Nick, UUID    string
	AccessToken   string // Microsoft account session ("" = offline)
	XUID          string
	RamMB         int
	Width, Height int
	Server        string // host[:port] to join right away ("" = main menu)
}

func offlineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name))
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	return hex.EncodeToString(h[:])
}

func dashedUUID(u string) string {
	if len(u) != 32 {
		return u
	}
	return u[:8] + "-" + u[8:12] + "-" + u[12:16] + "-" + u[16:20] + "-" + u[20:]
}

func buildArgs(r *Resolved, o LaunchOpts) ([]string, error) {
	libDir := filepath.Join(o.Root, "libraries")
	var cp []string
	seen := map[string]bool{}
	for _, l := range r.Libraries {
		if !rulesAllow(l.Rules) {
			continue
		}
		d, ok := l.artifact(libDir)
		if !ok || seen[d.Dest] {
			continue
		}
		seen[d.Dest] = true
		cp = append(cp, d.Dest)
	}
	jar := filepath.Join(o.Root, "versions", r.ID, r.ID+".jar")
	if !fileExists(jar) {
		jar = filepath.Join(o.Root, "versions", r.Base, r.Base+".jar")
	}
	cp = append(cp, jar)

	natives := filepath.Join(o.Root, "natives", r.ID)
	os.MkdirAll(natives, 0o755)
	if err := extractNatives(o.Root, r, natives); err != nil {
		return nil, err
	}
	assetsRoot := filepath.Join(o.Root, "assets")
	gameAssets := assetsRoot
	if ai, err := readAssetIndex(filepath.Join(assetsRoot, "indexes", r.Assets+".json")); err == nil && (ai.Virtual || ai.MapToResources) {
		gameAssets = legacyAssetsDir(o.Root, r)
		if ai.MapToResources {
			res := filepath.Join(o.GameDir, "resources")
			if !fileExists(res) {
				copyTree(gameAssets, res)
			}
		}
	}
	token, userType := "0", "legacy"
	if o.AccessToken != "" {
		token, userType = o.AccessToken, "msa"
	}
	vars := map[string]string{
		"auth_player_name":    o.Nick,
		"version_name":        r.ID,
		"game_directory":      o.GameDir,
		"assets_root":         assetsRoot,
		"game_assets":         gameAssets,
		"assets_index_name":   r.Assets,
		"auth_uuid":           o.UUID,
		"auth_access_token":   token,
		"auth_session":        token,
		"clientid":            "0",
		"auth_xuid":           firstNonEmpty(o.XUID, "0"),
		"user_type":           userType,
		"version_type":        "release",
		"natives_directory":   natives,
		"launcher_name":       "OurPlaceCraft",
		"launcher_version":    "2.0",
		"classpath":           strings.Join(cp, classpathSep),
		"classpath_separator": classpathSep,
		"library_directory":   libDir,
		"user_properties":     "{}",
		"profile_name":        o.Nick,
	}
	expand := func(s string) string {
		for k, v := range vars {
			s = strings.ReplaceAll(s, "${"+k+"}", v)
		}
		return s
	}
	collect := func(args []Arg) []string {
		var out []string
		for _, a := range args {
			if !rulesAllow(a.Rules) {
				continue
			}
			for _, v := range a.Values {
				out = append(out, expand(v))
			}
		}
		return out
	}
	args := []string{
		fmt.Sprintf("-Xmx%dM", o.RamMB),
		fmt.Sprintf("-Xms%dM", min(o.RamMB, 1024)),
		"-XX:+UseG1GC", "-XX:+UnlockExperimentalVMOptions", "-XX:G1NewSizePercent=20",
		"-XX:G1ReservePercent=20", "-XX:MaxGCPauseMillis=50", "-XX:G1HeapRegionSize=32M",
		"-Dfile.encoding=UTF-8",
	}
	if r.HasModernArgs && len(r.JVM) > 0 {
		args = append(args, collect(r.JVM)...)
	} else {
		args = append(args, "-Djava.library.path="+natives, "-cp", vars["classpath"])
	}
	args = append(args, r.MainClass)
	if r.HasModernArgs && len(r.Game) > 0 {
		args = append(args, collect(r.Game)...)
	}
	if r.MinecraftArguments != "" {
		for _, a := range strings.Fields(r.MinecraftArguments) {
			args = append(args, expand(a))
		}
	}
	if o.Width >= 854 && o.Height >= 480 {
		args = append(args, "--width", fmt.Sprint(o.Width), "--height", fmt.Sprint(o.Height))
	}
	if o.Server != "" {
		if r.supportsQuickPlay() {
			args = append(args, "--quickPlayMultiplayer", o.Server)
		} else {
			host, port, err := net.SplitHostPort(o.Server)
			if err != nil {
				host, port = o.Server, "25565"
			}
			args = append(args, "--server", host, "--port", port)
		}
	}
	for _, a := range args {
		if strings.Contains(a, "${") {
			return nil, fmt.Errorf("неизвестный параметр запуска: %s", a)
		}
	}
	return args, nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(t, 0o755)
		}
		return copyFile(p, t)
	})
}
