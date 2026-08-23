package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReloadEngine_updatesVersion(t *testing.T) {
	h := NewHolder(NewEngine(EngineInput{Version: "v1"}))
	require.Equal(t, "v1", h.Load().Version())

	ReloadEngine(h, EngineInput{Version: "v2"})
	require.Equal(t, "v2", h.Load().Version())
}

func TestReloadEngine_nilHolder(t *testing.T) {
	ReloadEngine(nil, EngineInput{Version: "x"})
}
