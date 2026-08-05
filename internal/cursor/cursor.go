// Package cursor persists a single opaque "last seen" value per input to a
// local JSON file, so a stateless oneshot poll can pick up where the
// previous run left off (e.g. `_id > last_seen` style incremental fetches).
package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type fileState struct {
	Value string `json:"value"`
}

// Read returns the persisted cursor value for path, or initialValue if the
// file does not exist yet.
func Read(path, initialValue string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return initialValue, nil
		}
		return "", fmt.Errorf("read cursor file %q: %w", path, err)
	}

	var state fileState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("parse cursor file %q: %w", path, err)
	}
	return state.Value, nil
}

// Write persists value to path atomically (write to a temp file, then
// rename), so a crash mid-write can never leave a corrupt cursor file.
func Write(path, value string) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create cursor dir for %q: %w", path, err)
		}
	}

	data, err := json.Marshal(fileState{Value: value})
	if err != nil {
		return fmt.Errorf("encode cursor value: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write cursor tmp file %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename cursor tmp file %q -> %q: %w", tmp, path, err)
	}
	return nil
}

// Max returns whichever of a, b sorts later. It compares numerically when
// both values parse as numbers (covers ids, unix timestamps, ...) and falls
// back to lexicographic string comparison otherwise (covers ISO-8601
// timestamps and any other monotonically sortable string cursor).
func Max(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}

	af, aErr := strconv.ParseFloat(a, 64)
	bf, bErr := strconv.ParseFloat(b, 64)
	if aErr == nil && bErr == nil {
		if af >= bf {
			return a
		}
		return b
	}

	if a >= b {
		return a
	}
	return b
}

// Stringify converts an extracted JSON value into the plain-text form used
// to store and compare cursor values.
func Stringify(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
	}
}
