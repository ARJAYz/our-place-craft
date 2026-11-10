package main

// Downloads, progress and archives — shared by every part of the launcher.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------- progress

type Progress struct {
	mu       sync.Mutex
	Stage    string
	Done     int64
	Total    int64
	Busy     bool
	Err      error
	Finished bool
}

func (p *Progress) set(stage string, total int64) {
	p.mu.Lock()
	p.Stage, p.Done, p.Total = stage, 0, total
	p.mu.Unlock()
}
func (p *Progress) add(n int64) { p.mu.Lock(); p.Done += n; p.mu.Unlock() }
func (p *Progress) snapshot() (string, int64, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Stage, p.Done, p.Total
}

// ---------------------------------------------------------------- downloads

var httpc = &http.Client{Timeout: 10 * time.Minute}

type dl struct {
	URL, Dest, SHA1 string
	Size            int64
}

func fileSHA1(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// fileOK reports whether dest already holds the expected content.
func fileOK(d dl) bool {
	st, err := os.Stat(d.Dest)
	if err != nil || st.IsDir() {
		return false
	}
	if d.Size > 0 && st.Size() != d.Size {
		return false
	}
	if d.SHA1 != "" && d.Size > 0 && d.Size < 64<<20 {
		return strings.EqualFold(fileSHA1(d.Dest), d.SHA1)
	}
	return st.Size() > 0
}

func download(d dl, onBytes func(int64)) error {
	if fileOK(d) {
		if onBytes != nil {
			onBytes(max(d.Size, 1))
		}
		return nil
	}
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		last = downloadOnce(d, onBytes)
		if last == nil {
			return nil
		}
	}
	return fmt.Errorf("не удалось скачать %s: %w", d.URL, last)
}

func downloadOnce(d dl, onBytes func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(d.Dest), 0o755); err != nil {
		return err
	}
	req, _ := http.NewRequest("GET", d.URL, nil)
	req.Header.Set("User-Agent", "OurPlaceCraftLauncher/1.0")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := d.Dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	var got int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			h.Write(buf[:n])
			got += int64(n)
			if onBytes != nil && d.Size > 0 {
				onBytes(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			if onBytes != nil && d.Size > 0 {
				onBytes(-got)
			}
			return rerr
		}
	}
	f.Close()
	if d.SHA1 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), d.SHA1) {
		os.Remove(tmp)
		if onBytes != nil && d.Size > 0 {
			onBytes(-got)
		}
		return errors.New("контрольная сумма не совпала")
	}
	if onBytes != nil && d.Size <= 0 {
		onBytes(1)
	}
	os.Remove(d.Dest)
	return os.Rename(tmp, d.Dest)
}

// downloadAll fetches files in parallel; progress is reported in bytes.
func downloadAll(list []dl, p *Progress, stage string) error {
	var total int64
	for _, d := range list {
		if d.Size > 0 {
			total += d.Size
		} else {
			total++
		}
	}
	p.set(stage, total)
	jobs := make(chan dl)
	var firstErr atomic.Value
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range jobs {
				if firstErr.Load() != nil {
					continue
				}
				if err := download(d, p.add); err != nil {
					firstErr.CompareAndSwap(nil, err)
				}
			}
		}()
	}
	for _, d := range list {
		jobs <- d
	}
	close(jobs)
	wg.Wait()
	if e := firstErr.Load(); e != nil {
		return e.(error)
	}
	return nil
}

// httpGet does a GET with the launcher's User-Agent; the caller closes the body.
func httpGet(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "OurPlaceCraftLauncher/2.0 (github.com/ARJAYz/our-place-craft)")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
	}
	return resp, nil
}

func getJSON(url string, v any) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "OurPlaceCraftLauncher/1.0")
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := httpc.Do(req)
		if err != nil {
			last = err
			time.Sleep(2 * time.Second)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				last = fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
				return
			}
			last = json.NewDecoder(resp.Body).Decode(v)
		}()
		if last == nil {
			return nil
		}
	}
	return last
}

// untarGz unpacks a .tar.gz keeping file modes and symlinks (needed for the Linux JRE).
func untarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	clean := filepath.Clean(dst) + string(os.PathSeparator)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(h.Name))
		if target != filepath.Clean(dst) && !strings.HasPrefix(target, clean) {
			continue // path traversal guard
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777|0o200)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		}
	}
}

func unzip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dst, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) {
			continue // zip-slip guard
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
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

// ---------------------------------------------------------------- vanilla + NeoForge

// ---------------------------------------------------------------- files

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
