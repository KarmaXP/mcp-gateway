package namespace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinOpaqueSplitOpaqueRoundTrip(t *testing.T) {
	a, err := JoinOpaque("alpha", "file:///x/y")
	if err != nil {
		t.Fatal(err)
	}
	p, n, err := SplitOpaque(a)
	if err != nil || p != "alpha" || n != "file:///x/y" {
		t.Fatalf("got %q %q %v", p, n, err)
	}
}

func TestJoinOpaqueEncodesWhenSeparatorInURI(t *testing.T) {
	raw := "https://ex.com/a__b"
	a, err := JoinOpaque("pre", raw)
	if err != nil {
		t.Fatal(err)
	}
	p, n, err := SplitOpaque(a)
	if err != nil || p != "pre" || n != raw {
		t.Fatalf("got %q %q err=%v", p, n, err)
	}
}

func TestJoinOpaqueEscapesAValueThatAlreadyLooksEncoded(t *testing.T) {
	raw := "gw0:abc"

	joined, err := JoinOpaque("alpha", raw)
	require.NoError(t, err)

	prefix, native, err := SplitOpaque(joined)
	require.NoError(t, err)
	require.Equal(t, "alpha", prefix)
	require.Equal(t, raw, native,
		"an unescaped gw0: value decodes as base64 and hands the caller a different resource")
}

func TestOpaqueRoundTripsEveryShapeAURICanTake(t *testing.T) {
	natives := []string{
		"file:///x/y",
		"https://ex.com/a__b",
		"gw0:abc",
		"gw0:",
		"__leading",
		"trailing__",
		"a__b__c",
		"plain",
		"spaces and ünïcode",
	}
	for _, raw := range natives {
		t.Run(raw, func(t *testing.T) {
			joined, err := JoinOpaque("pre", raw)
			require.NoError(t, err)

			prefix, native, err := SplitOpaque(joined)
			require.NoError(t, err)
			require.Equal(t, "pre", prefix)
			require.Equal(t, raw, native)
		})
	}
}

func TestJoinOpaqueRejectsAnEmptyValue(t *testing.T) {
	_, err := JoinOpaque("alpha", "")
	require.ErrorIs(t, err, ErrInvalidToolName)
}

func TestSplitOpaqueReportsAnUndecodableSegment(t *testing.T) {
	_, _, err := SplitOpaque("alpha__gw0:not!valid!base64")
	require.ErrorIs(t, err, ErrInvalidToolName)
}
