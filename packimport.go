package main

// Importing modpacks: Modrinth (.mrpack) and CurseForge (.zip with manifest.json).
// A new instance is created with the pack's Minecraft version and loader, mods are downloaded,
// and overrides are unpacked into the game folder.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ImportResult struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Mods   int      `json:"mods"`
	Manual []Manual `json:"manual,omitempty"` // mods that must be downloaded by hand
}

type Manual struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

var cfAPI = "https://api.curseforge.com/v1"

// ImportPack runs the import of an uploaded file in the background (job id).
func (a *App) ImportPack(uploadID string) int {
	return a.async(func() (any, error) {
		a.mu.Lock()
		if a.busy != "" {
			a.mu.Unlock()
			return nil, errors.New("подождите, идёт установка или игра")
		}
		a.busy, a.status, a.isErr = "import", "Импортируем сборку…", false
		a.mu.Unlock()
		res, err := a.importPack(uploadPath(a.store.Root, uploadID))
		os.Remove(uploadPath(a.store.Root, uploadID))
		if err != nil {
			a.finish("Импорт не удался: "+err.Error(), true)
			return nil, err
		}
		msg := fmt.Sprintf("Сборка «%s» импортирована: модов %d", res.Name, res.Mods)
		if len(res.Manual) > 0 {
			msg += fmt.Sprintf(", вручную скачать: %d", len(res.Manual))
		}
		a.finish(msg, false)
		a.Select(res.ID)
		return res, nil
	})
}

func uploadPath(root, id string) string { return filepath.Join(root, "downloads", "upload-"+id+".zip") }

func (a *App) importPack(file string) (*ImportResult, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, errors.New("это не архив сборки (.mrpack или .zip)")
	}
	defer zr.Close()
	for _, f := range zr.File {
		switch f.Name {
		case "modrinth.index.json":
			return a.importModrinth(&zr.Reader, f)
		case "manifest.json":
			return a.importCurseForge(&zr.Reader, f)
		}
	}
	return nil, errors.New("в архиве нет modrinth.index.json или manifest.json — это не сборка Modrinth/CurseForge")
}

func readZipJSON(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(io.LimitReader(rc, 32<<20)).Decode(v)
}

// ---------------------------------------------------------------- Modrinth

type mrIndex struct {
	FormatVersion int    `json:"formatVersion"`
	Game          string `json:"game"`
	Name          string `json:"name"`
	VersionID     string `json:"versionId"`
	Files         []struct {
		Path      string            `json:"path"`
		Hashes    map[string]string `json:"hashes"`
		Env       map[string]string `json:"env"`
		Downloads []string          `json:"downloads"`
		FileSize  int64             `json:"fileSize"`
	} `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
}

func (a *App) importModrinth(zr *zip.Reader, f *zip.File) (*ImportResult, error) {
	var idx mrIndex
	if err := readZipJSON(f, &idx); err != nil {
		return nil, fmt.Errorf("modrinth.index.json повреждён: %w", err)
	}
	if idx.Game != "" && idx.Game != "minecraft" {
		return nil, errors.New("сборка не для Minecraft")
	}
	mc := idx.Dependencies["minecraft"]
	if mc == "" {
		return nil, errors.New("в сборке не указана версия Minecraft")
	}
	loader, lv := LoaderVanilla, ""
	for k, v := range idx.Dependencies {
		switch k {
		case "fabric-loader":
			loader, lv = LoaderFabric, v
		case "quilt-loader":
			loader, lv = LoaderQuilt, v
		case "neoforge":
			loader, lv = LoaderNeoForge, v
		case "forge":
			loader, lv = LoaderForge, mc+"-"+v
		}
	}
	name := strings.TrimSpace(idx.Name)
	if idx.VersionID != "" {
		name += " " + idx.VersionID
	}
	in, err := a.store.Create(NewInstance{Name: name, MC: mc, Loader: loader, LoaderVersion: lv})
	if err != nil {
		return nil, err
	}
	game := a.store.GameDir(in)
	var list []dl
	for _, fl := range idx.Files {
		if fl.Env["client"] == "unsupported" || len(fl.Downloads) == 0 {
			continue
		}
		dst, ok := safeJoin(game, fl.Path)
		if !ok {
			continue
		}
		list = append(list, dl{URL: fl.Downloads[0], Dest: dst, SHA1: fl.Hashes["sha1"], Size: fl.FileSize})
	}
	if err := downloadAll(list, a.prog, "Скачиваем моды сборки"); err != nil {
		a.store.Delete(in.ID)
		return nil, err
	}
	a.prog.set("Распаковываем настройки сборки", 0)
	if err := extractOverrides(zr, game, "overrides/", "client-overrides/"); err != nil {
		return nil, err
	}
	return &ImportResult{ID: in.ID, Name: in.Name, Mods: len(list)}, nil
}

// ---------------------------------------------------------------- CurseForge

type cfManifest struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Overrides string `json:"overrides"`
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	Files []struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	} `json:"files"`
}

func (a *App) importCurseForge(zr *zip.Reader, f *zip.File) (*ImportResult, error) {
	var m cfManifest
	if err := readZipJSON(f, &m); err != nil {
		return nil, fmt.Errorf("manifest.json повреждён: %w", err)
	}
	mc := m.Minecraft.Version
	if mc == "" {
		return nil, errors.New("в сборке не указана версия Minecraft")
	}
	loader, lv := LoaderVanilla, ""
	for _, l := range m.Minecraft.ModLoaders {
		if !l.Primary && len(m.Minecraft.ModLoaders) > 1 {
			continue
		}
		switch {
		case strings.HasPrefix(l.ID, "forge-"):
			loader, lv = LoaderForge, mc+"-"+strings.TrimPrefix(l.ID, "forge-")
		case strings.HasPrefix(l.ID, "neoforge-"):
			loader, lv = LoaderNeoForge, strings.TrimPrefix(l.ID, "neoforge-")
		case strings.HasPrefix(l.ID, "fabric-"):
			loader, lv = LoaderFabric, strings.TrimPrefix(l.ID, "fabric-")
		case strings.HasPrefix(l.ID, "quilt-"):
			loader, lv = LoaderQuilt, strings.TrimPrefix(l.ID, "quilt-")
		}
	}
	name := strings.TrimSpace(m.Name + " " + m.Version)
	in, err := a.store.Create(NewInstance{Name: name, MC: mc, Loader: loader, LoaderVersion: lv})
	if err != nil {
		return nil, err
	}
	game := a.store.GameDir(in)
	res := &ImportResult{ID: in.ID, Name: in.Name}
	var ids []int
	for _, fl := range m.Files {
		ids = append(ids, fl.FileID)
	}
	key := a.store.Settings().CFKey
	byID := map[int]cfFile{}
	if key != "" && len(ids) > 0 {
		a.prog.set("Спрашиваем CurseForge о файлах", 0)
		files, err := cfFiles(key, ids)
		if err != nil {
			return nil, err
		}
		for _, x := range files {
			byID[x.ID] = x
		}
	}
	var list []dl
	for _, fl := range m.Files {
		x, ok := byID[fl.FileID]
		if !ok || x.DownloadURL == "" || x.FileName == "" {
			res.Manual = append(res.Manual, Manual{Name: firstNonEmpty(x.DisplayName, x.FileName, "мод "+strconv.Itoa(fl.ProjectID)),
				URL: fmt.Sprintf("https://www.curseforge.com/projects/%d", fl.ProjectID)})
			continue
		}
		dst, ok := safeJoin(game, path.Join(cfFolder(x), x.FileName))
		if !ok {
			continue
		}
		d := dl{URL: x.DownloadURL, Dest: dst, Size: x.FileLength}
		for _, h := range x.Hashes {
			if h.Algo == 1 { // sha1
				d.SHA1 = h.Value
			}
		}
		list = append(list, d)
	}
	if err := downloadAll(list, a.prog, "Скачиваем моды сборки"); err != nil {
		a.store.Delete(in.ID)
		return nil, err
	}
	res.Mods = len(list)
	ov := strings.Trim(firstNonEmpty(m.Overrides, "overrides"), "/") + "/"
	a.prog.set("Распаковываем настройки сборки", 0)
	if err := extractOverrides(zr, game, ov); err != nil {
		return nil, err
	}
	if len(res.Manual) > 0 {
		writeJSON(filepath.Join(game, "СКАЧАТЬ ВРУЧНУЮ.json"), res.Manual)
	}
	return res, nil
}

type cfFile struct {
	ID          int    `json:"id"`
	DisplayName string `json:"displayName"`
	FileName    string `json:"fileName"`
	DownloadURL string `json:"downloadUrl"`
	FileLength  int64  `json:"fileLength"`
	Hashes      []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	ModID int `json:"modId"`
}

// cfFolder: resource packs and shaders go to their folders, everything else to mods.
func cfFolder(f cfFile) string {
	n := strings.ToLower(f.FileName)
	if strings.HasSuffix(n, ".zip") {
		if strings.Contains(n, "shader") {
			return "shaderpacks"
		}
		return "resourcepacks"
	}
	return "mods"
}

func cfFiles(key string, ids []int) ([]cfFile, error) {
	body, _ := json.Marshal(map[string]any{"fileIds": ids})
	req, _ := http.NewRequest("POST", cfAPI+"/mods/files", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	var out struct {
		Data []cfFile `json:"data"`
	}
	code, err := doJSON(req, &out)
	if err != nil {
		return nil, fmt.Errorf("нет связи с CurseForge: %w", err)
	}
	if code == 403 || code == 401 {
		return nil, errors.New("CurseForge не принял ключ API — проверьте его в «Настройках»")
	}
	if code != 200 {
		return nil, fmt.Errorf("CurseForge ответил %d", code)
	}
	return out.Data, nil
}

// ---------------------------------------------------------------- helpers

func doJSON(req *http.Request, out any) (int, error) {
	req.Header.Set("User-Agent", "OurPlaceCraftLauncher/2.0")
	resp, err := httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if out != nil && len(b) > 0 {
		json.Unmarshal(b, out)
	}
	return resp.StatusCode, nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// safeJoin keeps paths from the pack inside the game folder.
func safeJoin(base, rel string) (string, bool) {
	rel = filepath.FromSlash(path.Clean("/" + strings.ReplaceAll(rel, "\\", "/")))[1:]
	if rel == "" {
		return "", false
	}
	p := filepath.Join(base, rel)
	if !strings.HasPrefix(p, filepath.Clean(base)+string(os.PathSeparator)) {
		return "", false
	}
	return p, true
}

func extractOverrides(zr *zip.Reader, game string, prefixes ...string) error {
	for _, pre := range prefixes {
		for _, f := range zr.File {
			if !strings.HasPrefix(f.Name, pre) || strings.HasSuffix(f.Name, "/") {
				continue
			}
			dst, ok := safeJoin(game, strings.TrimPrefix(f.Name, pre))
			if !ok {
				continue
			}
			os.MkdirAll(filepath.Dir(dst), 0o755)
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.Create(dst)
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
	}
	return nil
}

// saveUpload stores a pack file sent by the page and returns its id.
func saveUpload(root string, r io.Reader) (string, error) {
	id := strconv.FormatInt(time.Now().UnixNano(), 36)
	p := uploadPath(root, id)
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(r, 2<<30)); err != nil {
		return "", err
	}
	return id, nil
}

// OpenURL opens an https link in the player's browser.
func (a *App) OpenURL(u string) error {
	if !strings.HasPrefix(u, "https://") {
		return errors.New("можно открыть только https-ссылку")
	}
	openURL(u)
	return nil
}

func (a *App) SetCFKey(key string) error {
	st := a.store.Settings()
	st.CFKey = strings.TrimSpace(key)
	return a.store.SaveSettings(st)
}
