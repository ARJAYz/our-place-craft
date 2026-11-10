package main

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Files the player owns: an update never overwrites them if they already exist.
var keepIfPresent = map[string]bool{
	"options.txt":                       true,
	"servers.dat":                       true,
	"kubejs/client_scripts/opc_skin.js": true, // written by the launcher on every start
}

// Folders whose contents are fully managed by the pack: files the pack no longer
// ships are removed on update (only files directly inside, matching the extension).
var managedDirs = []struct{ dir, ext string }{
	{"mods", ".jar"},
	{"kubejs/client_scripts", ".js"},
	{"kubejs/server_scripts", ".js"},
	{"kubejs/startup_scripts", ".js"},
	{".opc-tweaks/xaero_world", ".jar"}, // optional mods shipped with the pack (see tweaks.go)
	{".opc-tweaks/xaero_mini", ".jar"},
}

// Files that older packs shipped and that must disappear on update (replaced by newer versions).
var obsoleteFiles = []string{
	"shaderpacks/photon_v1.3b.zip",
	"shaderpacks/photon_v1.3b.zip.txt",
}

// Mods taken out of the pack after a release (they break the server). The launcher never installs
// them and deletes them from mods, even when the pack on GitHub still ships them.
//   carryon: the server kicked everyone nearby when a player died
//   (EncoderException: Failed to encode packet 'clientbound/minecraft:custom_payload'), 30.10.2026.
var bannedMods = []string{"carryon-*.jar"}

func isBanned(rel string) bool {
	rel = strings.ToLower(path.Clean(filepath.ToSlash(rel)))
	if !strings.HasPrefix(rel, "mods/") || strings.Count(rel, "/") != 1 {
		return false
	}
	for _, g := range bannedMods {
		if ok, _ := path.Match(g, strings.TrimPrefix(rel, "mods/")); ok {
			return true
		}
	}
	return false
}

// removeBannedMods deletes the banned mods from a game folder; returns their names.
func removeBannedMods(game string) []string {
	var out []string
	for _, g := range bannedMods {
		m, _ := filepath.Glob(filepath.Join(game, "mods", g))
		for _, f := range m {
			if os.Remove(f) == nil {
				out = append(out, filepath.Base(f))
			}
		}
	}
	return out
}

type UpdateStats struct {
	Written, Removed, Same int
	WrittenNames, RemovedNames []string // only filled in repair mode
}

// SyncOpts: repair mode moves extra mods to mods-removed (instead of deleting) and keeps the
// optional mods the launcher's tweaks put into mods.
type SyncOpts struct {
	Repair bool
}

// packManifest remembers size+CRC of every file the pack installed, so the launcher can check
// the files later without downloading the pack again.
type packManifest map[string]struct {
	Size int64  `json:"size"`
	CRC  uint32 `json:"crc"`
}

const manifestName = ".opc-pack-manifest.json"

func loadManifest(game string) packManifest {
	m := packManifest{}
	if readJSONFile(filepath.Join(game, manifestName), &m) != nil {
		return nil
	}
	return m
}

// tweakJarNames: jars the tweaks copy into mods (cached in .opc-tweaks) — not "extra" mods.
func tweakJarNames(game string) map[string]bool {
	out := map[string]bool{}
	filepath.WalkDir(filepath.Join(game, tweaksDir), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".jar") {
			out[strings.ToLower(d.Name())] = true
		}
		return nil
	})
	return out
}

func fileCRC(p string) (uint32, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	n, err := io.Copy(h, f)
	return h.Sum32(), n, err
}

// syncPack brings dst in line with the pack inside the installer: only changed or new
// files are written, removed mods/scripts are deleted, worlds and player settings stay.
func (p *Payload) syncPack(dst string, prog *Progress, opts ...SyncOpts) (UpdateStats, error) {
	var st UpdateStats
	var o SyncOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	man := packManifest{}
	var total int64
	inPack := map[string]bool{}
	for _, f := range p.Zip.File {
		name := entryName(f.Name)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		inPack[strings.ToLower(path.Clean(name))] = true
		total += int64(f.UncompressedSize64)
		if top := strings.Split(path.Clean(name), "/")[0]; !strings.EqualFold(top, "saves") && !isBanned(name) {
			man[path.Clean(name)] = struct {
				Size int64  `json:"size"`
				CRC  uint32 `json:"crc"`
			}{int64(f.UncompressedSize64), f.CRC32}
		}
	}
	prog.set("Проверяем и обновляем файлы сборки", total)
	clean := filepath.Clean(dst) + string(os.PathSeparator)
	for _, f := range p.Zip.File {
		name := entryName(f.Name)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		name = path.Clean(name)
		size := int64(f.UncompressedSize64)
		top := strings.Split(name, "/")[0]
		target := filepath.Join(dst, filepath.FromSlash(name))
		if strings.EqualFold(top, "saves") || !strings.HasPrefix(target, clean) || isBanned(name) {
			prog.add(size)
			continue
		}
		if fi, err := os.Stat(target); err == nil {
			if keepIfPresent[strings.ToLower(name)] {
				st.Same++
				prog.add(size)
				continue
			}
			if fi.Size() == size {
				if c, _, err := fileCRC(target); err == nil && c == f.CRC32 {
					st.Same++
					prog.add(size)
					continue
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return st, err
		}
		rc, err := f.Open()
		if err != nil {
			return st, fmt.Errorf("архив сборки повреждён (%s): %w", name, err)
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return st, fmt.Errorf("не удалось записать %s — закройте Minecraft и попробуйте снова (%v)", name, err)
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return st, fmt.Errorf("ошибка распаковки %s: %w", name, err)
		}
		st.Written++
		if o.Repair {
			st.WrittenNames = append(st.WrittenNames, name)
		}
		prog.add(size)
	}
	for _, rel := range obsoleteFiles {
		if !inPack[strings.ToLower(rel)] && os.Remove(filepath.Join(dst, filepath.FromSlash(rel))) == nil {
			st.Removed++
		}
	}
	tweakJars := map[string]bool{}
	if o.Repair {
		tweakJars = tweakJarNames(dst)
	}
	for _, m := range managedDirs {
		entries, err := os.ReadDir(filepath.Join(dst, filepath.FromSlash(m.dir)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), m.ext) {
				continue
			}
			rel := m.dir + "/" + e.Name()
			if inPack[strings.ToLower(rel)] || keepIfPresent[strings.ToLower(rel)] {
				continue
			}
			if o.Repair {
				if tweakJars[strings.ToLower(e.Name())] || strings.HasPrefix(m.dir, tweaksDir) {
					continue
				}
				bak := filepath.Join(dst, "mods-removed", filepath.FromSlash(rel))
				os.MkdirAll(filepath.Dir(bak), 0o755)
				os.Remove(bak)
				if os.Rename(filepath.Join(dst, filepath.FromSlash(rel)), bak) == nil {
					st.Removed++
					st.RemovedNames = append(st.RemovedNames, e.Name())
				}
				continue
			}
			if os.Remove(filepath.Join(dst, filepath.FromSlash(rel))) == nil {
				st.Removed++
			}
		}
	}
	st.Removed += len(removeBannedMods(dst))
	writeJSON(filepath.Join(dst, manifestName), man)
	return st, nil
}
