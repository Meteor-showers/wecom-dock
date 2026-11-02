package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func unmarshalState(raw []byte, dest *state) error {
	return json.Unmarshal(raw, dest)
}

// PathFor returns the state file inside dir.
func PathFor(dir string) string {
	return filepath.Join(dir, "state.json")
}
