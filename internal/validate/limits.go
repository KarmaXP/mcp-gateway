// Size/shape limits on tools/call JSON before schema validation.
package validate

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

// ErrArgumentsTooLarge is returned when the raw JSON arguments exceed MaxBytes (use with errors.Is).
var ErrArgumentsTooLarge = errors.New("validate: arguments exceed max size")

type Limits struct {
	MaxBytes int
	MaxDepth int
	MaxKeys  int
}

func DefaultLimits() Limits {
	return Limits{
		MaxBytes: defaults.MaxToolArgumentsJSONBytes,
		MaxDepth: defaults.MaxToolArgumentsJSONDepth,
		MaxKeys:  defaults.MaxToolArgumentsJSONKeys,
	}
}

func CheckArgumentJSON(raw json.RawMessage, lim Limits) error {
	if lim.MaxBytes <= 0 {
		lim = DefaultLimits()
	}
	if len(raw) > lim.MaxBytes {
		return fmt.Errorf("%w (%d bytes; limit %d)", ErrArgumentsTooLarge, len(raw), lim.MaxBytes)
	}
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("arguments are not valid JSON: %w", err)
	}
	if lim.MaxDepth > 0 {
		if d := jsonDepth(v, 1); d > lim.MaxDepth {
			return fmt.Errorf("arguments exceed max nesting depth (%d)", lim.MaxDepth)
		}
	}
	if lim.MaxKeys > 0 {
		if n := jsonKeyCount(v); n > lim.MaxKeys {
			return fmt.Errorf("arguments exceed max key count (%d)", lim.MaxKeys)
		}
	}
	return nil
}

func jsonDepth(v any, cur int) int {
	switch t := v.(type) {
	case map[string]any:
		max := cur
		for _, vv := range t {
			max = maxInt(max, jsonDepth(vv, cur+1))
		}
		return max
	case []any:
		max := cur
		for _, vv := range t {
			max = maxInt(max, jsonDepth(vv, cur+1))
		}
		return max
	default:
		return cur
	}
}

func jsonKeyCount(v any) int {
	switch t := v.(type) {
	case map[string]any:
		n := len(t)
		for _, vv := range t {
			n += jsonKeyCount(vv)
		}
		return n
	case []any:
		n := 0
		for _, vv := range t {
			n += jsonKeyCount(vv)
		}
		return n
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
