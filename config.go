package main

// A config file, because a desktop launcher does not inherit your shell.
//
// Exported variables in ~/.bashrc reach a terminal, not a .desktop entry, so a
// UI configured only through the environment works when you start it by hand
// and silently falls back to defaults when you click the icon -- which shows up
// as "bkn unreachable at 127.0.0.1:7799" and looks like a broken install.
//
// Precedence is flags > environment > this file > defaults. The file only fills
// in what the environment has not already set, so nothing here can override an
// explicit choice made at the command line.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// configKeys is an allow-list: a config file is not a place to inject arbitrary
// environment into a process.
var configKeys = []string{
	"ESSAIM_URL",
	"BKN_URL",
	"ESSAIM_UI_DIR",
	"ESSAIM_UI_PORT",
	"ESSAIM_UI_NO_AUTH",
	"ESSAIM_UI_EMAIL",
	"ESSAIM_UI_PASSWORD",
	"ESSAIM_UI_ORG",
}

func known(k string) bool {
	for _, c := range configKeys {
		if c == k {
			return true
		}
	}
	return false
}

func configPath() string {
	base := env("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), ".config"))
	return filepath.Join(base, "essaim-ui", "config.json")
}

func readConfig() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(configPath())
	if err != nil {
		return m
	}
	raw := map[string]string{}
	if json.Unmarshal(b, &raw) != nil {
		return m
	}
	for k, v := range raw {
		if known(k) {
			m[k] = v
		}
	}
	return m
}

// loadConfig applies the file to this process's environment, without touching
// anything the caller already set.
func loadConfig() {
	for k, v := range readConfig() {
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func writeConfig(m map[string]string) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// 0600: this can hold a password.
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func redact(k, v string) string {
	if strings.Contains(k, "PASSWORD") && v != "" {
		return "(set)"
	}
	return v
}

// config shows or edits the file: `config` prints it, `config KEY=VALUE ...`
// sets, and an empty VALUE removes a key.
func config(args []string) {
	m := readConfig()
	changed := false
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			fail(exitUsage, "invalid_value", "expected KEY=VALUE, got "+a,
				"essaim-ui config BKN_URL=http://127.0.0.1:7799")
		}
		k = strings.ToUpper(strings.TrimSpace(k))
		if !known(k) {
			fail(exitUsage, "unknown_key", "not a config key: "+k,
				"known keys: "+strings.Join(configKeys, ", "))
		}
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
		changed = true
	}
	if changed {
		if err := writeConfig(m); err != nil {
			fail(exitUnavailable, "write_failed", err.Error())
		}
	}
	shown := map[string]string{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		shown[k] = redact(k, m[k])
	}
	out(map[string]any{"ok": true, "path": configPath(), "config": shown,
		"note": "flags > environment > this file > defaults"})
}
