package main

// Mod loaders: Fabric and Quilt (ready profiles from their meta servers), NeoForge and Forge
// (their official installers). Every installer writes a version into <root>/versions.

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	LoaderVanilla  = "vanilla"
	LoaderFabric   = "fabric"
	LoaderQuilt    = "quilt"
	LoaderNeoForge = "neoforge"
	LoaderForge    = "forge"
)

var (
	fabricMeta  = "https://meta.fabricmc.net/v2"
	quiltMeta   = "https://meta.quiltmc.org/v3"
	neoMaven    = "https://maven.neoforged.net/releases/net/neoforged/neoforge/"
	forgeMaven  = "https://maven.minecraftforge.net/net/minecraftforge/forge/"
	loaderNames = map[string]string{LoaderVanilla: "Без загрузчика", LoaderFabric: "Fabric", LoaderQuilt: "Quilt", LoaderNeoForge: "NeoForge", LoaderForge: "Forge"}
)

type LoaderVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// loaderVersions lists the loader builds for a Minecraft version (newest / recommended first).
func loaderVersions(loader, mc string) ([]LoaderVersion, error) {
	switch loader {
	case LoaderFabric, LoaderQuilt:
		base := fabricMeta
		if loader == LoaderQuilt {
			base = quiltMeta
		}
		var raw []struct {
			Loader struct {
				Version string `json:"version"`
				Stable  *bool  `json:"stable"`
			} `json:"loader"`
		}
		if err := getJSON(base+"/versions/loader/"+mc, &raw); err != nil {
			return nil, fmt.Errorf("нет связи с сервером %s: %w", loaderNames[loader], err)
		}
		var out []LoaderVersion
		for _, r := range raw {
			stable := r.Loader.Stable == nil || *r.Loader.Stable
			if loader == LoaderQuilt {
				stable = !strings.Contains(r.Loader.Version, "-")
			}
			out = append(out, LoaderVersion{r.Loader.Version, stable})
		}
		return out, nil
	case LoaderNeoForge, LoaderForge:
		all, err := mavenVersions(loader)
		if err != nil {
			return nil, err
		}
		var out []LoaderVersion
		for _, v := range all {
			if lv, ok := matchLoaderVersion(loader, mc, v); ok {
				out = append(out, lv)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return versionLess(out[j].Version, out[i].Version) })
		return out, nil
	}
	return nil, nil
}

// matchLoaderVersion decides whether a maven version of NeoForge/Forge belongs to the Minecraft version.
// NeoForge: MC 1.21.1 -> 21.1.x, 1.21 -> 21.0.x, 1.21.11 -> 21.11.x. Forge: "1.20.1-47.3.0".
func matchLoaderVersion(loader, mc, v string) (LoaderVersion, bool) {
	if loader == LoaderForge {
		if !strings.HasPrefix(v, mc+"-") {
			return LoaderVersion{}, false
		}
		rest := strings.TrimPrefix(v, mc+"-")
		if strings.Contains(rest, "-") { // e.g. 1.7.10-10.13.4.1614-1.7.10 — keep, it is still that MC
			return LoaderVersion{v, true}, true
		}
		return LoaderVersion{v, true}, true
	}
	prefix := neoPrefix(mc)
	if prefix == "" || !strings.HasPrefix(v, prefix) {
		return LoaderVersion{}, false
	}
	return LoaderVersion{v, !strings.Contains(v, "beta") && !strings.Contains(v, "alpha")}, true
}

func neoPrefix(mc string) string {
	if !strings.HasPrefix(mc, "1.") {
		return mc + "." // new numbering (26.1 …)
	}
	p := strings.Split(strings.TrimPrefix(mc, "1."), ".")
	if len(p) == 1 {
		return p[0] + ".0."
	}
	return p[0] + "." + p[1] + "."
}

func mavenVersions(loader string) ([]string, error) {
	base := neoMaven
	if loader == LoaderForge {
		base = forgeMaven
	}
	resp, err := httpGet(base + "maven-metadata.xml")
	if err != nil {
		return nil, fmt.Errorf("нет связи с сервером %s: %w", loaderNames[loader], err)
	}
	defer resp.Body.Close()
	var md struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&md); err != nil {
		return nil, err
	}
	return md.Versions, nil
}

// versionLess compares dotted versions numerically ("21.1.9" < "21.1.10").
func versionLess(a, b string) bool {
	sa := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	sb := strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	for i := 0; i < len(sa) && i < len(sb); i++ {
		na, ea := strconv.Atoi(sa[i])
		nb, eb := strconv.Atoi(sb[i])
		if ea == nil && eb == nil {
			if na != nb {
				return na < nb
			}
			continue
		}
		if sa[i] != sb[i] {
			return sa[i] < sb[i]
		}
	}
	return len(sa) < len(sb)
}

// loaderVersionID is the name of the version folder the loader creates.
func loaderVersionID(loader, mc, lv string) string {
	switch loader {
	case LoaderFabric:
		return "fabric-loader-" + lv + "-" + mc
	case LoaderQuilt:
		return "quilt-loader-" + lv + "-" + mc
	case LoaderNeoForge:
		return "neoforge-" + lv
	case LoaderForge:
		return mc + "-forge-" + strings.TrimPrefix(lv, mc+"-")
	}
	return mc
}

// installLoader puts the loader's version profile into <root>/versions and returns its id.
func installLoader(root, loader, mc, lv, java string, p *Progress, logw io.Writer) (string, error) {
	switch loader {
	case LoaderVanilla, "":
		return mc, nil
	case LoaderFabric, LoaderQuilt:
		base := fabricMeta
		if loader == LoaderQuilt {
			base = quiltMeta
		}
		p.set("Получаем профиль "+loaderNames[loader]+" "+lv, 0)
		var raw json.RawMessage
		if err := getJSON(base+"/versions/loader/"+mc+"/"+lv+"/profile/json", &raw); err != nil {
			return "", fmt.Errorf("не удалось получить профиль %s: %w", loaderNames[loader], err)
		}
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
			return "", errors.New("профиль " + loaderNames[loader] + " повреждён")
		}
		dst := filepath.Join(root, "versions", head.ID, head.ID+".json")
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return head.ID, os.WriteFile(dst, raw, 0o644)
	case LoaderNeoForge, LoaderForge:
		id := loaderVersionID(loader, mc, lv)
		if _, err := readVersion(root, id); err == nil {
			return id, nil
		}
		url := neoMaven + lv + "/neoforge-" + lv + "-installer.jar"
		flag := "--install-client"
		if loader == LoaderForge {
			url = forgeMaven + lv + "/forge-" + lv + "-installer.jar"
			flag = "--installClient"
		}
		inst := filepath.Join(root, "downloads", filepath.Base(url))
		if err := downloadAll([]dl{{URL: url, Dest: inst}}, p, "Скачиваем "+loaderNames[loader]+" "+lv); err != nil {
			return "", err
		}
		prof := filepath.Join(root, "launcher_profiles.json")
		if !fileExists(prof) {
			os.WriteFile(prof, []byte(`{"profiles":{},"selectedProfile":"","clientToken":""}`), 0o644)
		}
		if java == "" {
			return "", errors.New("нет Java для установщика " + loaderNames[loader])
		}
		before := versionDirs(root)
		p.set("Устанавливаем "+loaderNames[loader]+" (это пара минут)…", 0)
		cmd := newCmd(java, "-jar", inst, flag, root)
		cmd.Dir = filepath.Join(root, "downloads")
		cmd.Stdout, cmd.Stderr = logw, logw
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("установщик %s завершился с ошибкой (%v), подробности в logs/install.log", loaderNames[loader], err)
		}
		os.Remove(inst)
		if _, err := readVersion(root, id); err == nil {
			return id, nil
		}
		for d := range versionDirs(root) { // the installer may name the folder differently
			if !before[d] && strings.Contains(d, strings.TrimPrefix(lv, mc+"-")) {
				return d, nil
			}
		}
		return "", errors.New(loaderNames[loader] + " не установился, подробности в logs/install.log")
	}
	return "", errors.New("неизвестный загрузчик " + loader)
}

func versionDirs(root string) map[string]bool {
	m := map[string]bool{}
	es, _ := os.ReadDir(filepath.Join(root, "versions"))
	for _, e := range es {
		if e.IsDir() && fileExists(filepath.Join(root, "versions", e.Name(), e.Name()+".json")) {
			m[e.Name()] = true
		}
	}
	return m
}
