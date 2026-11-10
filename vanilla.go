package main

// Installing any Minecraft version from Mojang: version list, client, libraries, natives, assets.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	assetsURL   = "https://resources.download.minecraft.net/"
)

type MCVersion struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // release, snapshot, old_beta, old_alpha
	URL         string    `json:"url"`
	SHA1        string    `json:"sha1"`
	ReleaseTime time.Time `json:"releaseTime"`
}

var (
	manifestMu    sync.Mutex
	manifestCache []MCVersion
)

// mcVersions returns every Minecraft version Mojang publishes (newest first).
func mcVersions() ([]MCVersion, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifestCache != nil {
		return manifestCache, nil
	}
	var man struct {
		Versions []MCVersion `json:"versions"`
	}
	if err := getJSON(manifestURL, &man); err != nil {
		return nil, fmt.Errorf("нет связи с серверами Mojang: %w", err)
	}
	manifestCache = man.Versions
	return manifestCache, nil
}

// ensureVersionJSON downloads versions/<id>/<id>.json of a vanilla version if it is missing.
func ensureVersionJSON(root, id string) error {
	vjson := filepath.Join(root, "versions", id, id+".json")
	if fileExists(vjson) {
		return nil
	}
	list, err := mcVersions()
	if err != nil {
		return err
	}
	for _, v := range list {
		if v.ID == id {
			return download(dl{URL: v.URL, Dest: vjson, SHA1: v.SHA1}, nil)
		}
	}
	return errors.New("версия Minecraft " + id + " не найдена у Mojang")
}

// installGameFiles downloads everything a resolved version needs: client, libraries, natives, assets.
func installGameFiles(root string, r *Resolved, p *Progress) error {
	libDir := filepath.Join(root, "libraries")
	var list []dl
	if r.ClientDL != nil {
		list = append(list, dl{URL: r.ClientDL.URL, Dest: filepath.Join(root, "versions", r.Base, r.Base+".jar"), SHA1: r.ClientDL.SHA1, Size: r.ClientDL.Size})
	}
	for _, l := range r.Libraries {
		if !rulesAllow(l.Rules) {
			continue
		}
		if d, ok := l.artifact(libDir); ok && d.URL != "" {
			list = append(list, d)
		} else if ok && !fileExists(d.Dest) {
			return errors.New("библиотека " + l.Name + " не установлена и её негде скачать")
		}
		if d, ok := l.native(libDir); ok {
			list = append(list, d)
		}
	}
	if err := downloadAll(list, p, "Скачиваем Minecraft "+r.Base); err != nil {
		return err
	}
	if err := ensureLaunchJar(root, r); err != nil {
		return err
	}
	return installAssets(root, r, p)
}

// ensureLaunchJar puts a copy of the vanilla client into versions/<id>/<id>.jar, as the official
// launcher does (Forge/NeoForge refer to this exact file).
func ensureLaunchJar(root string, r *Resolved) error {
	if r.ID == r.Base {
		return nil
	}
	src := filepath.Join(root, "versions", r.Base, r.Base+".jar")
	dst := filepath.Join(root, "versions", r.ID, r.ID+".jar")
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return nil
	}
	return copyFile(src, dst)
}

func installAssets(root string, r *Resolved, p *Progress) error {
	if r.AssetIndex == nil {
		return errors.New("в описании версии нет assetIndex")
	}
	idx := filepath.Join(root, "assets", "indexes", r.AssetIndex.ID+".json")
	if err := download(dl{URL: r.AssetIndex.URL, Dest: idx, SHA1: r.AssetIndex.SHA1, Size: r.AssetIndex.Size}, nil); err != nil {
		return err
	}
	ai, err := readAssetIndex(idx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var list []dl
	for _, o := range ai.Objects {
		if seen[o.Hash] || len(o.Hash) < 2 {
			continue
		}
		seen[o.Hash] = true
		list = append(list, dl{URL: assetsURL + o.Hash[:2] + "/" + o.Hash,
			Dest: filepath.Join(root, "assets", "objects", o.Hash[:2], o.Hash), SHA1: o.Hash, Size: o.Size})
	}
	if err := downloadAll(list, p, "Скачиваем звуки и текстуры"); err != nil {
		return err
	}
	if ai.Virtual || ai.MapToResources { // very old versions read assets as plain files
		dst := legacyAssetsDir(root, r)
		p.set("Готовим ресурсы старой версии", 0)
		for name, o := range ai.Objects {
			t := filepath.Join(dst, filepath.FromSlash(name))
			if fileExists(t) {
				continue
			}
			if err := copyFile(filepath.Join(root, "assets", "objects", o.Hash[:2], o.Hash), t); err != nil {
				return err
			}
		}
	}
	return nil
}

type assetIndex struct {
	Virtual        bool `json:"virtual"`
	MapToResources bool `json:"map_to_resources"`
	Objects        map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
}

func readAssetIndex(p string) (*assetIndex, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	ai := &assetIndex{}
	return ai, json.Unmarshal(b, ai)
}

func legacyAssetsDir(root string, r *Resolved) string {
	id := "legacy"
	if r.AssetIndex != nil {
		id = r.AssetIndex.ID
	}
	return filepath.Join(root, "assets", "virtual", id)
}

// extractNatives unpacks the natives of old versions (before 1.19) into dir.
func extractNatives(root string, r *Resolved, dir string) error {
	libDir := filepath.Join(root, "libraries")
	for _, l := range r.Libraries {
		if !rulesAllow(l.Rules) {
			continue
		}
		d, ok := l.native(libDir)
		if !ok {
			continue
		}
		var exclude []string
		if l.Extract != nil {
			exclude = l.Extract.Exclude
		}
		if err := unzipFiltered(d.Dest, dir, exclude); err != nil {
			return fmt.Errorf("не удалось распаковать %s: %w", filepath.Base(d.Dest), err)
		}
	}
	return nil
}

func unzipFiltered(src, dst string, exclude []string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	clean := filepath.Clean(dst) + string(os.PathSeparator)
next:
	for _, f := range zr.File {
		for _, e := range exclude {
			if strings.HasPrefix(f.Name, e) {
				continue next
			}
		}
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		t := filepath.Join(dst, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(t, clean) {
			continue
		}
		if fileExists(t) {
			continue
		}
		os.MkdirAll(filepath.Dir(t), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(t)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// missingFiles lists what is not on disk for the resolved version (fast check before a start).
func missingFiles(root string, r *Resolved) []string {
	var miss []string
	libDir := filepath.Join(root, "libraries")
	for _, l := range r.Libraries {
		if !rulesAllow(l.Rules) {
			continue
		}
		if d, ok := l.artifact(libDir); ok && !fileExists(d.Dest) {
			miss = append(miss, d.Dest)
		}
		if d, ok := l.native(libDir); ok && !fileExists(d.Dest) {
			miss = append(miss, d.Dest)
		}
	}
	jar := filepath.Join(root, "versions", r.ID, r.ID+".jar")
	if r.ID == r.Base {
		jar = filepath.Join(root, "versions", r.Base, r.Base+".jar")
	}
	if !fileExists(jar) {
		miss = append(miss, jar)
	}
	if r.AssetIndex == nil || !fileExists(filepath.Join(root, "assets", "indexes", r.AssetIndex.ID+".json")) {
		miss = append(miss, "assets index")
	}
	return miss
}
