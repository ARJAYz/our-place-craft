// Package packcrypt encrypts/decrypts the modpack archive with an access code.
// Format: "OPCP1\n" | salt(16) | repeated { len uint32 BE | AES-256-GCM ciphertext of up to 1 MiB }.
// Key = PBKDF2-SHA256(code, salt). Nonce = chunk counter; AAD = 1 if last chunk. Truncation is detected.
package packcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

const (
	magic     = "OPCP1\n"
	chunkSize = 1 << 20
	iters     = 200000
)

var (
	ErrBadCode = errors.New("неверный код доступа")
	ErrCorrupt = errors.New("файл сборки повреждён или скачан не полностью")
)

func newGCM(code string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, code, salt, iters, 32)
	if err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func nonce(i uint64) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], i)
	return n
}

func Encrypt(dst io.Writer, src io.Reader, code string) error {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	g, err := newGCM(code, salt)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(dst, magic); err != nil {
		return err
	}
	if _, err := dst.Write(salt); err != nil {
		return err
	}
	cur := make([]byte, chunkSize)
	next := make([]byte, chunkSize)
	n, err := io.ReadFull(src, cur)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	var idx uint64
	for {
		var m int
		final := byte(1)
		if err == nil { // cur is full, peek next
			m, err = io.ReadFull(src, next)
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return err
			}
			if m > 0 {
				final = 0
			}
		}
		ct := g.Seal(nil, nonce(idx), cur[:n], []byte{final})
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(ct)))
		if _, e := dst.Write(l[:]); e != nil {
			return e
		}
		if _, e := dst.Write(ct); e != nil {
			return e
		}
		if final == 1 {
			return nil
		}
		cur, next = next, cur
		n = m
		idx++
	}
}

// Decrypt writes the plaintext to dst. onBytes receives the number of encrypted bytes consumed.
func Decrypt(dst io.Writer, src io.Reader, code string, onBytes func(int)) error {
	hdr := make([]byte, len(magic)+16)
	if _, err := io.ReadFull(src, hdr); err != nil || string(hdr[:len(magic)]) != magic {
		return ErrCorrupt
	}
	g, err := newGCM(code, hdr[len(magic):])
	if err != nil {
		return err
	}
	var idx uint64
	buf := make([]byte, chunkSize+64)
	var l [4]byte
	// read-ahead of one length so we know which chunk is last
	for {
		if _, err := io.ReadFull(src, l[:]); err != nil {
			return ErrCorrupt
		}
		sz := int(binary.BigEndian.Uint32(l[:]))
		if sz < g.Overhead() || sz > len(buf) {
			return ErrCorrupt
		}
		if _, err := io.ReadFull(src, buf[:sz]); err != nil {
			return ErrCorrupt
		}
		pt, err := g.Open(nil, nonce(idx), buf[:sz], []byte{0})
		last := false
		if err != nil {
			pt, err = g.Open(nil, nonce(idx), buf[:sz], []byte{1})
			last = true
		}
		if err != nil {
			if idx == 0 {
				return ErrBadCode
			}
			return ErrCorrupt
		}
		if _, err := dst.Write(pt); err != nil {
			return err
		}
		if onBytes != nil {
			onBytes(4 + sz)
		}
		if last {
			var one [1]byte
			if n, _ := src.Read(one[:]); n != 0 {
				return ErrCorrupt
			}
			return nil
		}
		idx++
	}
}
