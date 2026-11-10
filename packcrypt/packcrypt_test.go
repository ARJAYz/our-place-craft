package packcrypt

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 17} {
		data := make([]byte, n)
		rand.Read(data)
		var enc bytes.Buffer
		if err := Encrypt(&enc, bytes.NewReader(data), "TEST-CODE-1"); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := Decrypt(&out, bytes.NewReader(enc.Bytes()), "TEST-CODE-1", nil); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("n=%d mismatch", n)
		}
		if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(enc.Bytes()), "wrong", nil); !errors.Is(err, ErrBadCode) {
			t.Fatalf("n=%d wrong code: %v", n, err)
		}
		if n > chunkSize { // truncation must be detected
			b := enc.Bytes()
			cut := b[:len(b)-5000]
			if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(cut), "TEST-CODE-1", nil); err == nil {
				t.Fatal("truncated file accepted")
			}
			// dropping the whole last chunk must be detected too
			first := len(magic) + 16 + 4 + chunkSize + 16
			if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(b[:first]), "TEST-CODE-1", nil); err == nil {
				t.Fatal("dropped last chunk accepted")
			}
		}
	}
}
