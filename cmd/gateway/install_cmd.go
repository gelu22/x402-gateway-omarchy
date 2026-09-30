//go:build linux

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gateway/internal/install"
)

// runInstallCmd handles `gateway install` and `gateway self-remove`.
func runInstallCmd(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gateway install|self-remove [flags]")
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	home := fs.String("home", os.Getenv("HOME"), "HOME root for install layout")
	bundle := fs.String("bundle", "", "extracted plugin-bundle directory")
	binSrc := fs.String("binary", "", "verified gateway binary to install (default: this executable)")
	force := fs.Bool("force", false, "overwrite foreign files")
	keepState := fs.Bool("keep-state", false, "self-remove: keep state dir")
	keepConfig := fs.Bool("keep-config", false, "self-remove: keep plugin config")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	opts := install.Options{
		BundleDir: *bundle,
		BinarySrc: *binSrc,
		BinDir:    filepath.Join(*home, ".local", "bin"),
		StateDir:  filepath.Join(*home, ".local", "state", "x402-gateway"),
		ShareDir:  filepath.Join(*home, ".local", "share", "x402-gateway"),
		PluginDir: filepath.Join(*home, ".config", "omarchy", "plugins", install.DefaultPluginID),
		ConfigDir: filepath.Join(*home, ".config", "omarchy", "x402-gateway"),
		PluginID:  install.DefaultPluginID,
		Force:     *force || os.Getenv("GATEWAY_FORCE") == "1",
		Logger:    logger,
	}
	if xdg := os.Getenv("XDG_STATE_DIR"); xdg != "" {
		opts.StateDir = filepath.Join(xdg, "x402-gateway")
	}
	switch cmd {
	case "install":
		if opts.BundleDir == "" {
			fmt.Fprintln(os.Stderr, "install: --bundle required")
			return 2
		}
		if opts.BinarySrc == "" {
			exe, err := os.Executable()
			if err != nil {
				logger.Error("executable", "err", err)
				return 1
			}
			opts.BinarySrc, _ = filepath.EvalSymlinks(exe)
		}
		if err := install.Install(opts); err != nil {
			logger.Error("install", "err", err)
			fmt.Fprintln(os.Stderr, "✗", err)
			return 1
		}
		fmt.Println("✓ installed", filepath.Join(opts.BinDir, "gateway"))
		fmt.Println("  state dir:", opts.StateDir)
		return 0
	case "self-remove":
		ro := install.RemoveOptions{
			Options:    opts,
			KeepState:  *keepState,
			KeepConfig: *keepConfig,
		}
		if err := install.SelfRemove(ro); err != nil {
			logger.Error("self-remove", "err", err)
			fmt.Fprintln(os.Stderr, "✗", err)
			return 1
		}
		fmt.Println("✓ removed gateway program files")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown install command:", cmd)
		return 2
	}
}
