
package fsx

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func AtomicWrite(path string, data []byte) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func AppendJSONLine(path string, v any) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(v)
}

func WriteLines(path string, lines []string) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, ln := range lines {
		if _, err := io.WriteString(w, ln); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}

func Timestamp() string {
	return time.Now().UTC().Format("20060102T150405Z")
}

func Glob(dir, pattern string) ([]string, error) {
	full := filepath.Join(dir, pattern)
	return filepath.Glob(full)
}

func Debugf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
