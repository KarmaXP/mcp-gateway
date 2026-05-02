package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckArgumentJSON_DepthAndKeys(t *testing.T) {
	lim := Limits{MaxBytes: 1024, MaxDepth: 3, MaxKeys: 8}
	deep, _ := json.Marshal(map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": map[string]any{"d": 1},
			},
		},
	})
	err := CheckArgumentJSON(deep, lim)
	require.Error(t, err)

	wide := make(map[string]any)
	for i := range 20 {
		wide[fmt.Sprintf("k%d", i)] = 1
	}
	wideRaw, _ := json.Marshal(wide)
	err = CheckArgumentJSON(wideRaw, lim)
	require.Error(t, err)
}

func TestCheckArgumentJSON_AcceptsSmallObject(t *testing.T) {
	lim := Limits{MaxBytes: 1024, MaxDepth: 8, MaxKeys: 32}
	raw, _ := json.Marshal(map[string]any{"msg": "hi"})
	require.NoError(t, CheckArgumentJSON(raw, lim))
}

func TestCheckArgumentJSON_ErrArgumentsTooLarge(t *testing.T) {
	lim := Limits{MaxBytes: 4, MaxDepth: 8, MaxKeys: 32}
	raw := json.RawMessage(`{"a":1}`)
	err := CheckArgumentJSON(raw, lim)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrArgumentsTooLarge), "%v", err)
}
