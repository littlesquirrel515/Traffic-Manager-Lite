// Package secretstore seals managed authentication with a separately supplied key.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func Key(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("absolute managed key file required")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 256 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("managed key file must be a private regular file")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if len(b) != 32 {
		b, e = hex.DecodeString(strings.TrimSpace(string(b)))
	}
	if e != nil || len(b) != 32 {
		return nil, errors.New("managed encryption key must contain 32 bytes")
	}
	return b, nil
}
func Seal(key, b []byte) (string, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	sealed := g.Seal(nonce, nonce, b, []byte("TML-managed-v1"))
	return "tmlenc1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}
func Open(key []byte, s string) ([]byte, error) {
	if !strings.HasPrefix(s, "tmlenc1:") {
		return nil, errors.New("encrypted credential required")
	}
	b, e := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, "tmlenc1:"))
	if e != nil {
		return nil, errors.New("invalid encrypted material")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil || len(b) < g.NonceSize() {
		return nil, errors.New("invalid encrypted material")
	}
	return g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte("TML-managed-v1"))
}
