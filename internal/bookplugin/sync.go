package bookplugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Cylunex/shadow-relay/internal/security"
)

type Receipt struct {
	SetID string `json:"setId"`
	Hash  string `json:"hash"`
}
type SyncResult struct {
	Installed int `json:"installed"`
	Removed   int `json:"removed"`
	Unchanged int `json:"unchanged"`
}

// Install writes only directories bearing a matching Relay ownership receipt.
// Each replacement is an atomic rename with rollback; the host reload happens later.
// Unsupported rules never become code, and foreign/manual plugin files are untouched.
func Install(root string, report Report) (SyncResult, error) {
	var result SyncResult
	if report.Schema != "shadow.hub.plugins/v1" || !identifier.MatchString(report.SetID) || len(report.Entries) > 500 {
		return result, errors.New("invalid published plugin manifest")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return result, err
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real != root {
		return result, errors.New("plugin root may not contain symlinks")
	}
	// A directory lock is portable and fails closed on overlapping syncs. A stale
	// lock after a crash is left for explicit operator removal, never stolen.
	lock := filepath.Join(root, ".relay-sync-lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return result, errors.New("another sync is active or its lock needs recovery")
	}
	defer os.Remove(lock)
	wanted := map[string]bool{}
	prepared := map[string]map[string][]byte{}
	for _, entry := range report.Entries {
		if entry.Recipe == nil || len(entry.Blockers) > 0 {
			continue
		}
		if wanted[entry.ID] {
			return result, errors.New("duplicate plugin identifier")
		}
		wanted[entry.ID] = true
		files, e := Files(entry, report.GeneratedAt)
		if e != nil {
			return result, e
		}
		prepared[entry.ID] = files
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return result, err
	}
	owned := map[string]Receipt{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "relay_") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if wanted[entry.Name()] {
				return result, errors.New("refusing to replace a symlink plugin")
			}
			continue
		}
		if !entry.IsDir() {
			continue
		}
		b, e := os.ReadFile(filepath.Join(root, entry.Name(), ".shadow-relay.json"))
		var receipt Receipt
		if e == nil && json.Unmarshal(b, &receipt) == nil && receipt.SetID == report.SetID {
			owned[entry.Name()] = receipt
		} else if wanted[entry.Name()] {
			return result, errors.New("plugin directory belongs to another publisher or was created manually")
		}
	}
	ids := []string{}
	for id := range prepared {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		files := prepared[id]
		serialized, _ := json.Marshal(files)
		hash := security.Hash(serialized)
		if previous, ok := owned[id]; ok && previous.Hash == hash {
			result.Unchanged++
			continue
		}
		stage, e := os.MkdirTemp(filepath.Dir(root), ".relay-stage-")
		if e != nil {
			return result, e
		}
		err = func() error {
			defer os.RemoveAll(stage)
			for name, body := range files {
				path := filepath.Join(stage, name)
				if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
					return e
				}
				if e := os.WriteFile(path, body, 0600); e != nil {
					return e
				}
			}
			receipt, _ := json.Marshal(Receipt{SetID: report.SetID, Hash: hash})
			if e := os.WriteFile(filepath.Join(stage, ".shadow-relay.json"), receipt, 0600); e != nil {
				return e
			}
			target := filepath.Join(root, id)
			backup := stage + "-previous"
			if _, ok := owned[id]; ok {
				if e := os.Rename(target, backup); e != nil {
					return e
				}
			} else if _, e := os.Lstat(target); !os.IsNotExist(e) {
				return errors.New("refusing to replace an existing unowned path")
			}
			if e := os.Rename(stage, target); e != nil {
				_ = os.Rename(backup, target)
				return e
			}
			return os.RemoveAll(backup)
		}()
		if err != nil {
			return result, err
		}
		result.Installed++
	}
	for id := range owned {
		if !wanted[id] {
			if err = os.RemoveAll(filepath.Join(root, id)); err != nil {
				return result, err
			}
			result.Removed++
		}
	}
	return result, nil
}
