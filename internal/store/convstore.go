package store

import (
	"os"
	"path/filepath"
)

type ConvStore struct {
	root string
}

func NewConvStore(root string) (*ConvStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &ConvStore{root: root}, nil
}

func (cs *ConvStore) PathFor(sopID string) string {
	return filepath.Join(cs.root, sopID+".json")
}
