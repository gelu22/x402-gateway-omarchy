//go:build linux

package install

import (
	"fmt"
	"os"
)

// openRoot scopes file ops under dir via os.Root (gosec G304/G703).
// refuseSymlink stays: Root follows in-tree symlinks; Lstat is the only
// guard against a symlink *at* the root path itself.
func openRoot(dir string) (*os.Root, error) {
	if dir == "" {
		return nil, fmt.Errorf("install: empty root dir")
	}
	if err := refuseSymlink(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := refuseSymlink(dir); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}
