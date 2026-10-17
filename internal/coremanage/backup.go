package coremanage

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"traffic-manager-lite/internal/secretstore"
)

func (m *Manager) backupKey() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := os.MkdirAll(m.StateDir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(m.StateDir, "backup.key")
	if _, e := os.Stat(path); os.IsNotExist(e) {
		b := make([]byte, 32)
		if _, e = rand.Read(b); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.Write(b)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			return nil, e
		}
	}
	return secretstore.Key(path)
}
func (m *Manager) writeBackup(path string, b []byte) error {
	key, e := m.backupKey()
	if e != nil {
		return e
	}
	sealed, e := secretstore.Seal(key, b)
	if e != nil {
		return e
	}
	return atomic(path, []byte(sealed), 0600)
}
func (m *Manager) readBackup(path string) ([]byte, error) {
	key, e := m.backupKey()
	if e != nil {
		return nil, e
	}
	if rejectLinks(path) != nil {
		return nil, errors.New("invalid backup path")
	}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, errors.New("backup exceeds bound")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if len(b) > 16<<20 {
		return nil, errors.New("backup exceeds bound")
	}
	if !strings.HasPrefix(string(b), "tmlenc1:") {
		// Convert only a journal-authorized legacy backup with its recorded hash.
		authorized := false
		files, _ := filepath.Glob(filepath.Join(m.StateDir, "instance-*.json"))
		for _, file := range files {
			var j journal
			raw, err := os.ReadFile(file)
			if err == nil && json.Unmarshal(raw, &j) == nil && filepath.Clean(j.Backup) == filepath.Clean(path) && j.Old == Hash(b) {
				authorized = true
				break
			}
		}
		if !authorized {
			return nil, errors.New("legacy backup lacks matching journal evidence")
		}
		if e = m.writeBackup(path, b); e != nil {
			return nil, e
		}
		return b, nil
	}
	return secretstore.Open(key, string(b))
}
