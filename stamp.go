package main

// A "stamped" installer carries the access code in a small trailer at the very end of the exe:
//   [exe][xor(code)][len byte][ "OPCK1" ]
// It is only light obfuscation: send stamped installers privately.

import (
	"bytes"
	"errors"
	"os"
)

const stampMagic = "OPCK1"

func stampKey(i int) byte { return byte(0x5A + i*31) }

func stampBytes(code string) ([]byte, error) {
	if len(code) == 0 || len(code) > 120 {
		return nil, errors.New("код должен быть от 1 до 120 символов")
	}
	b := []byte(code)
	for i := range b {
		b[i] ^= stampKey(i)
	}
	b = append(b, byte(len(code)))
	return append(b, stampMagic...), nil
}

func readStampFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < int64(len(stampMagic)+2) {
		return ""
	}
	tail := make([]byte, 256)
	n := int64(len(tail))
	if st.Size() < n {
		n = st.Size()
	}
	if _, err := f.ReadAt(tail[:n], st.Size()-n); err != nil {
		return ""
	}
	tail = tail[:n]
	if !bytes.HasSuffix(tail, []byte(stampMagic)) {
		return ""
	}
	tail = tail[:len(tail)-len(stampMagic)]
	l := int(tail[len(tail)-1])
	tail = tail[:len(tail)-1]
	if l == 0 || l > len(tail) {
		return ""
	}
	b := append([]byte(nil), tail[len(tail)-l:]...)
	for i := range b {
		b[i] ^= stampKey(i)
	}
	return string(b)
}
