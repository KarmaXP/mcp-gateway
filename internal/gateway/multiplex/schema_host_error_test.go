package multiplex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostVisibleJSONSchemaError_OmitsInstanceValues(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
		"required": []any{"msg"},
	})
	require.NoError(t, err)
	sch, err := compileToolValidator("alpha__echo", raw)
	require.NoError(t, err)

	secret := "super-secret-token-value-12345"
	inst := map[string]any{"msg": 42, "extra": secret}
	err = sch.Validate(inst)
	require.Error(t, err)

	msg := hostVisibleJSONSchemaError(err)
	require.NotContains(t, msg, secret)
	require.Contains(t, msg, "schema validation failed")
}
