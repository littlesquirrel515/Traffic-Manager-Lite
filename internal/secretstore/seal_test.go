package secretstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateKeyHexAndAuthenticatedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	os.WriteFile(path, []byte(strings.Repeat("ab", 32)+"\n"), 0600)
	key, e := Key(path)
	if e != nil || len(key) != 32 {
		t.Fatal(e)
	}
	value, e := Seal(key, []byte("fixture-sensitive-auth"))
	if e != nil || strings.Contains(value, "fixture-sensitive-auth") {
		t.Fatal(e)
	}
	plain, e := Open(key, value)
	if e != nil || string(plain) != "fixture-sensitive-auth" {
		t.Fatal(e)
	}
	wrong := append([]byte(nil), key...)
	wrong[0] ^= 1
	if _, e = Open(wrong, value); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = Open(key, "fixture-sensitive-auth"); e == nil {
		t.Fatal("plaintext accepted")
	}
}
