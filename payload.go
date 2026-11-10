package main

// An installer can carry the modpack as a zip appended after the PE image:
//   [Setup.exe][pack.zip]
// The PE headers tell where the program ends, the rest is the zip.

import (
	"archive/zip"
	"debug/pe"
	"io"
	"os"
	"strings"
)

type Payload struct {
	f      *os.File
	ExeLen int64
	Zip    *zip.Reader
	// External: the zip is a separate file (downloaded), the exe itself carries no pack.
	External bool
}

// peLength returns the size of the PE image (end of the last section).
func peLength(f *os.File) (int64, error) {
	pf, err := pe.NewFile(f)
	if err != nil {
		return 0, err
	}
	var end int64
	for _, s := range pf.Sections {
		if e := int64(s.Offset) + int64(s.Size); e > end {
			end = e
		}
	}
	return end, nil
}

// openPayload returns nil (and no error) when the exe carries no pack.
func openPayload(exe string) (*Payload, error) {
	f, err := os.Open(exe)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	n, err := peLength(f)
	if err != nil || st.Size()-n < 22 {
		f.Close()
		return nil, nil
	}
	size := st.Size() - n
	zr, err := zip.NewReader(io.NewSectionReader(f, n, size), size)
	if err != nil {
		f.Close()
		return nil, nil
	}
	return &Payload{f: f, ExeLen: n, Zip: zr}, nil
}

func (p *Payload) Close() { p.f.Close() }

// entryName normalises zip entry names (Windows PowerShell 5 writes backslashes).
func entryName(n string) string {
	n = strings.ReplaceAll(n, `\`, "/")
	n = strings.TrimPrefix(n, "./")
	return strings.TrimLeft(n, "/")
}

func (p *Payload) hasMods() bool {
	for _, f := range p.Zip.File {
		n := strings.ToLower(entryName(f.Name))
		if strings.HasPrefix(n, "mods/") && strings.HasSuffix(n, ".jar") {
			return true
		}
	}
	return false
}
