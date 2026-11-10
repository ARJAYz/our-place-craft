package main

// Online pack: the installer without an embedded pack downloads an encrypted archive
// from the latest GitHub release and decrypts it with the access code on the fly.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opcl/packcrypt"
)

var (
	packBase    = "https://github.com/ARJAYz/our-place-craft/releases/latest/download/"
	stampedCode string // access code baked into this installer (see stamp.go)
)

const (
	packAsset = "pack.bin"
	metaAsset = "pack.json"
)

type PackMeta struct {
	Version string `json:"version"`
}

// dlClient has no overall timeout (the file is large), only a connect/header limit.
var dlClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	TLSHandshakeTimeout:   20 * time.Second,
	ResponseHeaderTimeout: 40 * time.Second,
}}

func httpGetOnline(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "OurPlaceCraftLauncher/1.0")
	resp, err := dlClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("нет связи с интернетом или GitHub недоступен: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, errors.New("сборка ещё не выложена (нет релиза на GitHub)")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub ответил HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// fetchMeta returns the version of the published pack; ok=false if it cannot be read.
func fetchMeta() (PackMeta, bool) {
	var m PackMeta
	resp, err := httpGetOnline(packBase + metaAsset)
	if err != nil {
		return m, false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(b, &m) != nil || m.Version == "" {
		return m, false
	}
	return m, true
}

// downloadPack streams pack.bin through the decryptor into destZip.
// A wrong access code fails on the first chunk, before the big download.
func downloadPack(code, destZip string, prog *Progress) error {
	if err := os.MkdirAll(filepath.Dir(destZip), 0o755); err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
		last = downloadPackOnce(code, destZip, prog)
		if last == nil || errors.Is(last, packcrypt.ErrBadCode) || strings.Contains(last.Error(), "ещё не выложена") {
			return last
		}
	}
	return last
}

type countingReader struct {
	r io.Reader
	n int64
	f func(int)
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	if n > 0 {
		c.n += int64(n)
		if c.f != nil {
			c.f(n)
		}
	}
	return n, err
}

func downloadPackOnce(code, destZip string, prog *Progress) error {
	resp, err := httpGetOnline(packBase + packAsset)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	prog.set("Скачиваем сборку", max(resp.ContentLength, 1))
	tmp := destZip + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	cr := &countingReader{r: resp.Body, f: func(n int) { prog.add(int64(n)) }}
	err = packcrypt.Decrypt(out, cr, code, nil)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(destZip)
	return os.Rename(tmp, destZip)
}

// openPackZip opens an unencrypted zip on disk as a Payload (no program part).
func openPackZip(p string) (*Payload, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("скачанный архив повреждён: %w", err)
	}
	pl := &Payload{f: f, Zip: zr, External: true}
	if !pl.hasMods() {
		f.Close()
		return nil, errors.New("в скачанной сборке нет модов")
	}
	return pl, nil
}
