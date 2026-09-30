//go:build linux

// Package install performs atomic lifecycle mutations for the gateway binary,
// QML plugin and helper scripts. Bash only downloads and verifies; every write
// and delete goes through Lstat + Openat(O_NOFOLLOW) + Renameat so a symlink
// swap between check and write cannot redirect the mutation (L18).
package install
