package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// persistHelpers: save/load circular buffer to JSON file (atomic write)
// saveAll writes both captures and color rules atomically.
func saveAll(path string, caps []Capture, rules []ColorRule) error {
	payload := PersistedData{
		Captures:   caps,
		ColorRules: rules,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadAll reads captures + rules. Back-compat: if the file is either a plain
// []Capture or an object containing only captures, we still succeed.
func loadAll(path string) ([]Capture, []ColorRule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	// Try new format first
	var pd PersistedData
	if err := json.Unmarshal(b, &pd); err == nil && (pd.Captures != nil || pd.ColorRules != nil) {
		return pd.Captures, pd.ColorRules, nil
	}

	return nil, nil, fmt.Errorf("unrecognized persistence format")
}
