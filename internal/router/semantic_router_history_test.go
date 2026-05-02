package router

import (
	"testing"

	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func TestTieBreakAmbiguousPairPrefersMoreRecentHistory(t *testing.T) {
	top := store.VectorSearchHit{ToolName: "a__t1", Score: 0.5}
	second := store.VectorSearchHit{ToolName: "a__t2", Score: 0.48}
	recent := []string{"a__t1", "a__t2"}
	nt, ns, ok := tieBreakAmbiguousPair(top, second, recent)
	if !ok {
		t.Fatal("expected tie-break")
	}
	if nt.ToolName != "a__t2" || ns.ToolName != "a__t1" {
		t.Fatalf("want t2 first (more recent), got %v %v", nt, ns)
	}

	nt2, ns2, ok2 := tieBreakAmbiguousPair(top, second, []string{"a__t1"})
	if !ok2 || nt2.ToolName != "a__t1" {
		t.Fatalf("single history should prefer t1: %v %v", nt2, ns2)
	}
}
