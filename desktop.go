package main

// Desktop integration: an icon in the launcher, and a command behind it that
// behaves like double-clicking an app rather than like starting a server.
//
// The .desktop file runs `essaim-ui open`, not `serve`, because a launcher
// entry is expected to be idempotent: clicking it twice should show the app
// twice, not fail with "address already in use". open probes first and only
// starts a server if nothing is answering.

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const desktopFile = "essaim-ui.desktop"

func uiURL(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

// alive reports whether something is already serving essaim-ui on the port.
// It checks /_health rather than just the socket, so a half-dead process or an
// unrelated listener does not look like a running UI.
func alive(port int) bool {
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := c.Get(uiURL(port) + "/_health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// open is the desktop entry point: make sure a UI is running, then show it.
func open(args []string) {
	port := 8687
	if p := env("ESSAIM_UI_PORT", ""); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	spawn := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				i++
				n, err := strconv.Atoi(args[i])
				if err != nil {
					fail(exitUsage, "invalid_value", "--port must be a number")
				}
				port = n
			}
		case "--no-spawn":
			spawn = false
		}
	}

	started := false
	if !alive(port) {
		if !spawn {
			fail(exitUnavailable, "not_running", "nothing is serving essaim-ui on port "+strconv.Itoa(port),
				"essaim-ui serve --no-auth --port "+strconv.Itoa(port))
		}
		if !portFree(port) {
			fail(exitUnavailable, "port_busy",
				"port "+strconv.Itoa(port)+" is taken by something that is not essaim-ui",
				"pick another with --port")
		}
		if err := spawnServer(port); err != nil {
			fail(exitUnavailable, "spawn_failed", err.Error())
		}
		// Wait for it to answer rather than racing the browser to the socket.
		deadline := time.Now().Add(10 * time.Second)
		for !alive(port) {
			if time.Now().After(deadline) {
				fail(exitUnavailable, "start_timeout",
					"the server did not answer on "+uiURL(port)+" within 10s",
					"run `essaim-ui serve --no-auth` in a terminal to see why")
			}
			time.Sleep(200 * time.Millisecond)
		}
		started = true
	}

	opened := true
	if err := exec.Command("xdg-open", uiURL(port)).Start(); err != nil {
		opened = false
	}
	out(map[string]any{"ok": true, "url": uiURL(port), "started": started, "opened": opened})
}

// spawnServer starts `serve --no-auth` detached, so the UI outlives the
// launcher process that clicked the icon.
func spawnServer(port int) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(os.TempDir(), "essaim-ui.log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(self, "serve", "--no-auth", "--port", strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = lf, lf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Do not Wait: the child is meant to outlive us.
	return nil
}

func appDirs() (apps, icons string) {
	data := env("XDG_DATA_HOME", filepath.Join(os.Getenv("HOME"), ".local", "share"))
	return filepath.Join(data, "applications"),
		filepath.Join(data, "icons", "hicolor", "scalable", "apps")
}

func installDesktop() {
	self, err := os.Executable()
	if err != nil {
		fail(exitUnavailable, "no_executable_path", err.Error())
	}
	self, _ = filepath.Abs(self)
	apps, icons := appDirs()
	for _, d := range []string{apps, icons} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fail(exitUnavailable, "mkdir_failed", err.Error())
		}
	}
	iconPath := filepath.Join(icons, "essaim-ui.svg")
	if err := os.WriteFile(iconPath, []byte(iconSVG), 0o644); err != nil {
		fail(exitUnavailable, "write_failed", err.Error())
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=essaim
GenericName=BitTorrent Client
Comment=Add magnets, cap speeds and watch downloads
Exec=%s open
Icon=essaim-ui
Terminal=false
Categories=Network;FileTransfer;P2P;
Keywords=torrent;magnet;bittorrent;download;
StartupNotify=true
`, self)
	entryPath := filepath.Join(apps, desktopFile)
	if err := os.WriteFile(entryPath, []byte(entry), 0o644); err != nil {
		fail(exitUnavailable, "write_failed", err.Error())
	}
	// Best effort: the entry works without it, it just may not appear until
	// the desktop rescans on its own.
	indexed := exec.Command("update-desktop-database", apps).Run() == nil
	out(map[string]any{"ok": true, "desktop_entry": entryPath, "icon": iconPath,
		"exec": self + " open", "indexed": indexed,
		"note":   "launches on 127.0.0.1 with --no-auth; the icon may take a moment to appear",
		"caveat": "Exec pins the binary's CURRENT path -- re-run install-desktop if you move it"})
}

func uninstallDesktop() {
	apps, icons := appDirs()
	removed := []string{}
	for _, p := range []string{filepath.Join(apps, desktopFile), filepath.Join(icons, "essaim-ui.svg")} {
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		}
	}
	_ = exec.Command("update-desktop-database", apps).Run()
	out(map[string]any{"ok": true, "removed": removed})
}
