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

// fileSHA256 returns the hex digest of path, or "" on error.
// Path may live outside StateDir (e.g. ~/.local/bin/gateway); we root the
// parent directory and open only filepath.Base (G304 sanitizer + os.Root).
func fileSHA256(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || strings.Contains(base, "..") {
		return "", fmt.Errorf("install: refused path %q", path)
	}
	root, err := openRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(base)
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

// IsOurs reports whether path is a regular non-symlink file whose sha matches
// the registry entry (same format as the legacy bash installer / sha256sum).
func IsOurs(stateDir, path string) bool {
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
	root, err := openRoot(stateDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(registryName)
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
	root, err := openRoot(stateDir)
	if err != nil {
		return err
	}
	defer root.Close()
	var kept []string
	if raw, err := root.ReadFile(registryName); err == nil {
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
	tmp := registryName + ".tmp"
	if err := root.WriteFile(tmp, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return root.Rename(tmp, registryName)
}

func registryClear(stateDir, path string) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	root, err := openRoot(stateDir)
	if err != nil {
		return err
	}
	defer root.Close()
	raw, err := root.ReadFile(registryName)
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
	tmp := registryName + ".tmp"
	body := ""
	if len(kept) > 0 {
		body = strings.Join(kept, "\n") + "\n"
	}
	if err := root.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return root.Rename(tmp, registryName)
}
