//go:build linux

package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// refuseSymlink fails if path exists and is a symlink (Lstat, never follows).
func refuseSymlink(path string) error {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink — refusing to write through it", path)
	}
	return nil
}

// installFileAtomically copies src to destDir/destName via a temp sibling,
// Openat(O_NOFOLLOW) + Renameat. Parent dir must not be a symlink.
func installFileAtomically(src, destDir, destName string, mode uint32) error {
	if err := refuseSymlink(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	if err := refuseSymlink(destDir); err != nil { // re-check after mkdir
		return err
	}
	dirfd, err := unix.Open(destDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("install: open dir %s: %w", destDir, err)
	}
	defer unix.Close(dirfd)

	tmpName := "." + destName + ".tmp"
	_ = unix.Unlinkat(dirfd, tmpName, 0)
	fd, err := unix.Openat(dirfd, tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return fmt.Errorf("install: openat %s: %w", tmpName, err)
	}
	tmpf := os.NewFile(uintptr(fd), filepath.Join(destDir, tmpName))
	in, err := os.Open(src)
	if err != nil {
		tmpf.Close()
		_ = unix.Unlinkat(dirfd, tmpName, 0)
		return err
	}
	_, copyErr := io.Copy(tmpf, in)
	in.Close()
	syncErr := tmpf.Sync()
	closeErr := tmpf.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = unix.Unlinkat(dirfd, tmpName, 0)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	if err := unix.Renameat(dirfd, tmpName, dirfd, destName); err != nil {
		_ = unix.Unlinkat(dirfd, tmpName, 0)
		return fmt.Errorf("install: renameat %s: %w", destName, err)
	}
	return nil
}

// removeOurs unlinks path only when it is ours (or Force). Uses Unlinkat after Lstat.
func removeOurs(stateDir, path string, force bool) error {
	if err := refuseSymlink(path); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		_ = registryClear(stateDir, path)
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("install: %s is not a regular file", path)
	}
	if !force && !isOurs(stateDir, path) {
		return fmt.Errorf("install: keeping %s (not installed by this installer)", path)
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	dirfd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirfd)
	// Re-Lstat via openat to shrink the TOCTOU window.
	fd, err := unix.Openat(dirfd, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	unix.Close(fd)
	if err := unix.Unlinkat(dirfd, base, 0); err != nil {
		return err
	}
	return registryClear(stateDir, path)
}

// ownedByUs reports the path's uid matches euid (when the file exists).
func ownedByUs(path string) error {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("install: %s owned by uid %d, not euid %d", path, stat.Uid, os.Geteuid())
	}
	return nil
}
