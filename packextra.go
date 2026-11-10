package main

// Pack extras: small additions to the Adventure pack published between full pack releases
// (new mods, quest files). They live in the `launcher` branch next to launcher.json:
//   pack-extra.json  {version, files:[{path, file, sha1, size}], remove:[glob]}
//   pack-extra/<file>
// The launcher installs them after the pack sync and records them in the pack manifest, so
// "Repair" treats them as part of the pack. Once a full pack ships the same files, nothing
// is downloaded (the SHA-1 already matches).

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type PackExtraFile struct {
	Path string `json:"path"` // relative to the game folder, e.g. mods/x.jar
	File string `json:"file"` // name under pack-extra/ in the branch
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type PackExtra struct {
	Version string          `json:"version"`
	Files   []PackExtraFile `json:"files"`
	Remove  []string        `json:"remove"` // globs relative to the game folder (mods/… only)
}

const packExtraCache = ".opc-pack-extra.json"

// fetchPackExtra reads pack-extra.json; falls back to the cached copy when offline.
var fetchPackExtra = func(game string) (*PackExtra, error) {
	var x PackExtra
	resp, err := httpGetOnline(launcherUpdateBase + "pack-extra.json")
	if err == nil {
		b, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if rerr == nil && json.Unmarshal(b, &x) == nil {
			os.WriteFile(filepath.Join(game, packExtraCache), b, 0o644)
			return &x, nil
		}
	}
	if readJSONFile(filepath.Join(game, packExtraCache), &x) == nil {
		return &x, nil
	}
	if err != nil && strings.Contains(err.Error(), "ещё не выложена") {
		return nil, nil // no extras published
	}
	return nil, err
}

func safeRel(game, rel string) (string, bool) {
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == "." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", false
	}
	t := filepath.Join(game, filepath.FromSlash(rel))
	if !strings.HasPrefix(t, filepath.Clean(game)+string(os.PathSeparator)) {
		return "", false
	}
	return t, true
}

// applyPackExtra installs the extras into game. Offline with nothing cached is not an error.
func applyPackExtra(game string, prog *Progress) (int, error) {
	x, err := fetchPackExtra(game)
	if err != nil || x == nil {
		return 0, nil
	}
	written := 0
	man := loadManifest(game)
	for _, g := range x.Remove {
		g = path.Clean(filepath.ToSlash(g))
		if !strings.HasPrefix(g, "mods/") || strings.Count(g, "/") != 1 {
			continue
		}
		m, _ := filepath.Glob(filepath.Join(game, filepath.FromSlash(g)))
		for _, f := range m {
			os.Remove(f)
			if man != nil {
				delete(man, "mods/"+filepath.Base(f))
			}
		}
	}
	var total int64
	for _, f := range x.Files {
		total += f.Size
	}
	prog.set("Дополнения к сборке", max(total, 1))
	for _, f := range x.Files {
		target, ok := safeRel(game, f.Path)
		if !ok || f.File == "" || strings.ContainsAny(f.File, `/\`) || isBanned(f.Path) {
			continue
		}
		want := strings.ToLower(f.SHA1)
		if fileSHA1(target) != want {
			if err := downloadExtra(f, target, want); err != nil {
				return written, err
			}
			written++
		}
		prog.add(f.Size)
		if man != nil {
			if c, n, err := fileCRC(target); err == nil {
				man[path.Clean(filepath.ToSlash(f.Path))] = struct {
					Size int64  `json:"size"`
					CRC  uint32 `json:"crc"`
				}{n, c}
			}
		}
	}
	if man != nil {
		writeJSON(filepath.Join(game, manifestName), man)
	}
	return written, nil
}

func downloadExtra(f PackExtraFile, target, want string) error {
	resp, err := httpGetOnline(launcherUpdateBase + "pack-extra/" + f.File)
	if err != nil {
		return fmt.Errorf("дополнение %s: %w", f.File, err)
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("не удалось записать %s — закройте Minecraft (%v)", f.Path, err)
	}
	h := sha1.New()
	_, err = io.Copy(io.MultiWriter(out, h), resp.Body)
	out.Close()
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = errors.New("файл повреждён при скачивании (SHA-1 не совпал)")
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("дополнение %s: %w", f.File, err)
	}
	os.Remove(target)
	return os.Rename(tmp, target)
}
