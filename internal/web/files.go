package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// removeData deletes the payload of one torrent: dir/name, which is a file for
// a single-file torrent and a directory for a multi-file one.
//
// This is the only destructive thing essaim-ui does, and essaim will not do it
// — `DELETE /torrents/<id>` drops the record and leaves the bytes. So the
// guards live here, and they are deliberately paranoid: dir and name both come
// from a daemon that got them from a user, and "delete recursively" plus a
// path you did not check is how a UI eats somebody's home directory.
func removeData(dir, name string) error {
	if strings.TrimSpace(name) == "" {
		// Before metadata resolves there is no name, so there is nothing this
		// function could safely delete — deleting `dir` itself would take the
		// whole download directory with it.
		return errors.New("this torrent has no resolved name yet, so its files cannot be identified")
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("torrent has no directory")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if absDir == "/" || absDir == filepath.Clean(os.Getenv("HOME")) {
		return fmt.Errorf("refusing to delete inside %s", absDir)
	}
	// A name is a single path element. Anything with a separator, or that
	// climbs, is refused rather than cleaned — silently "fixing" a hostile
	// name is how traversal bugs survive review.
	if name != filepath.Base(name) || name == "." || name == ".." {
		return fmt.Errorf("refusing to delete a suspicious name %q", name)
	}
	target := filepath.Join(absDir, name)
	// Belt and braces: the join must still land inside the directory.
	if !strings.HasPrefix(target, absDir+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to delete outside %s", absDir)
	}
	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			return nil // already gone; removing the entry is still the right outcome
		}
		return err
	}
	return os.RemoveAll(target)
}

// fullPath is what the UI shows, and it is a display value only.
func fullPath(dir, name string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if name == "" {
		return abs + string(os.PathSeparator)
	}
	return filepath.Join(abs, name)
}
