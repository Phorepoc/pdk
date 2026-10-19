package device

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Args are an op's arguments as decoded JSON. Numbers may arrive as JSON numbers or as strings
// ("0x01ca0000"), since addresses are easier to type in hex.
type Args map[string]any

func (a Args) Has(k string) bool { _, ok := a[k]; return ok }

func (a Args) String(k string) (string, error) {
	v, ok := a[k]
	if !ok {
		return "", fmt.Errorf("missing %q", k)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", k)
	}
	return s, nil
}

func (a Args) Uint(k string, bits int) (uint64, error) {
	v, ok := a[k]
	if !ok {
		return 0, fmt.Errorf("missing %q", k)
	}
	switch t := v.(type) {
	case float64:
		if t < 0 || t != float64(uint64(t)) {
			return 0, fmt.Errorf("%q must be a non-negative integer", k)
		}
		return checkBits(k, uint64(t), bits)
	case json.Number:
		return parseUint(k, t.String(), bits)
	case string:
		return parseUint(k, t, bits)
	case int:
		return checkBits(k, uint64(t), bits)
	case int64:
		return checkBits(k, uint64(t), bits)
	case uint32:
		return uint64(t), nil
	case uint64:
		return checkBits(k, t, bits)
	}
	return 0, fmt.Errorf("%q must be a number", k)
}

func (a Args) Uint32(k string) (uint32, error) {
	v, err := a.Uint(k, 32)
	return uint32(v), err
}

// UintOr returns def when k is absent.
func (a Args) UintOr(k string, bits int, def uint64) (uint64, error) {
	if !a.Has(k) {
		return def, nil
	}
	return a.Uint(k, bits)
}

func parseUint(k, s string, bits int) (uint64, error) {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseUint(s, 0, bits)
	if err != nil {
		return 0, fmt.Errorf("%q: %v", k, err)
	}
	return v, nil
}

func checkBits(k string, v uint64, bits int) (uint64, error) {
	if bits < 64 && v>>bits != 0 {
		return 0, fmt.Errorf("%q out of range", k)
	}
	return v, nil
}
