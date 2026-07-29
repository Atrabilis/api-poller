package extract

import (
	"fmt"
	"strconv"
	"strings"
)

type segment struct {
	key      string
	hasIndex bool
	index    int
}

func Resolve(root any, path string) (any, error) {
	segments, err := parsePath(path)
	if err != nil {
		return nil, err
	}

	current := root
	for _, seg := range segments {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("path %q expected object before key %q", path, seg.key)
		}

		next, ok := obj[seg.key]
		if !ok {
			return nil, fmt.Errorf("path %q missing key %q", path, seg.key)
		}

		if seg.hasIndex {
			arr, ok := next.([]any)
			if !ok {
				return nil, fmt.Errorf("path %q key %q is not an array", path, seg.key)
			}
			if seg.index < 0 || seg.index >= len(arr) {
				return nil, fmt.Errorf("path %q index %d out of range", path, seg.index)
			}
			next = arr[seg.index]
		}

		current = next
	}

	return current, nil
}

func ResolveItems(root any, path string) ([]any, error) {
	if strings.TrimSpace(path) == "" {
		return []any{root}, nil
	}

	value, err := Resolve(root, path)
	if err != nil {
		return nil, err
	}

	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("items_path %q did not resolve to an array", path)
	}

	return items, nil
}

func parsePath(path string) ([]segment, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("empty path")
	}

	trimmed = strings.TrimPrefix(trimmed, "$")
	trimmed = strings.TrimPrefix(trimmed, ".")
	if trimmed == "" {
		return nil, nil
	}

	rawSegments := strings.Split(trimmed, ".")
	segments := make([]segment, 0, len(rawSegments))
	for _, raw := range rawSegments {
		if raw == "" {
			continue
		}

		seg := segment{key: raw}
		if open := strings.Index(raw, "["); open >= 0 {
			close := strings.Index(raw, "]")
			if close < 0 || close < open {
				return nil, fmt.Errorf("invalid path segment %q", raw)
			}
			seg.key = raw[:open]
			indexValue := raw[open+1 : close]
			index, err := strconv.Atoi(indexValue)
			if err != nil {
				return nil, fmt.Errorf("invalid array index %q in segment %q", indexValue, raw)
			}
			seg.hasIndex = true
			seg.index = index
		}

		if seg.key == "" {
			return nil, fmt.Errorf("invalid path segment %q", raw)
		}
		segments = append(segments, seg)
	}

	return segments, nil
}
