//go:build !linux

package main

import (
	"fmt"
	"os"
)

func runInstallCmd(args []string) int {
	fmt.Fprintln(os.Stderr, "gateway install/self-remove: linux only")
	return 1
}
