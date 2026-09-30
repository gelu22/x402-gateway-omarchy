//go:build linux

package install

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const registryName = "installed.sha256"

var registryMu sync.Mutex

func registryPath(stateDir string) string {
	return filepath.Join(stateDir, registryName)
}

// fileSHA256 returns the hex digest of path, or "" on error.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isOurs reports whether path is a regular non-symlink file whose sha matches
// the registry entry (same format as the legacy bash installer).
func isOurs(stateDir, path string) bool {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	want, err := registryGet(stateDir, path)
	if err != nil || want == "" {
		return false
	}
	got, err := fileSHA256(path)
	return err == nil && got == want
}

func registryGet(stateDir, path string) (string, error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	f, err := os.Open(registryPath(stateDir))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	var last string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 67 {
			continue
		}
		if line[66:] == path {
			last = strings.Fields(line)[0]
		}
	}
	return last, sc.Err()
}

func registrySet(stateDir, path string) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	reg := registryPath(stateDir)
	var kept []string
	if raw, err := os.ReadFile(reg); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if line == "" {
				continue
			}
			if len(line) >= 67 && line[66:] == path {
				continue
			}
			kept = append(kept, line)
		}
	}
	kept = append(kept, fmt.Sprintf("%s  %s", sum, path))
	tmp := reg + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, reg)
}

func registryClear(stateDir, path string) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	reg := registryPath(stateDir)
	raw, err := os.ReadFile(reg)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		if len(line) >= 67 && line[66:] == path {
			continue
		}
		kept = append(kept, line)
	}
	tmp := reg + ".tmp"
	body := ""
	if len(kept) > 0 {
		body = strings.Join(kept, "\n") + "\n"
	}
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, reg)
}
