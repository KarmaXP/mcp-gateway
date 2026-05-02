package policy

import (
	"testing"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/stretchr/testify/require"
)

func TestHolder_LoadStore(t *testing.T) {
	e1 := NewEngine(config.PolicySettings{Version: "v1"})
	h := NewHolder(e1)
	require.Same(t, e1, h.Load())

	e2 := NewEngine(config.PolicySettings{Version: "v2"})
	h.Store(e2)
	require.Same(t, e2, h.Load())

	h.Store(nil)
	require.Nil(t, h.Load())
}

func TestHolder_nil_receiver(t *testing.T) {
	var h *Holder
	require.Nil(t, h.Load())
	h.Store(NewEngine(config.PolicySettings{})) // no-op
}
