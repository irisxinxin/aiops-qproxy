package store

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type SOPMap struct {
	mu   sync.RWMutex
	path string
	data map[string]string // incident_key -> sop_id
}

func LoadSOPMap(path string) (*SOPMap, error) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	m := &SOPMap{path: path, data: map[string]string{}}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &m.data)
	}
	return m, nil
}

func (m *SOPMap) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *SOPMap) GetOrCreate(key string) (string, error) {
	if v, ok := m.Get(key); ok {
		return v, nil
	}
	h := sha1.Sum([]byte(key))
	sop := "sop_" + hex.EncodeToString(h[:])[:12]

	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = sop
	return sop, m.saveLocked()
}

func (m *SOPMap) Set(key, sop string) error {
	if sop == "" {
		return errors.New("empty sop_id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = sop
	return m.saveLocked()
}

func (m *SOPMap) saveLocked() error {
	tmp := m.path + ".tmp"
	b, _ := json.MarshalIndent(m.data, "", "  ")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
