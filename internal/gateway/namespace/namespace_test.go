package namespace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinSplitRoundTrip(t *testing.T) {
	ns, err := Join("k8s", "get_logs")
	require.NoError(t, err)
	require.Equal(t, "k8s__get_logs", ns)

	p, native, err := Split(ns)
	require.NoError(t, err)
	require.Equal(t, "k8s", p)
	require.Equal(t, "get_logs", native)
}

func TestSplitRejectDoubleSeparatorInNative(t *testing.T) {
	_, _, err := Split("a__b__c")
	require.ErrorIs(t, err, ErrNativeContainsSep)
}

func TestResolveBackendOrder(t *testing.T) {
	m := map[string]string{"z": "id-z", "a": "id-a"}
	bid, native, err := ResolveBackend(m, "z__tool")
	require.NoError(t, err)
	require.Equal(t, "id-z", bid)
	require.Equal(t, "tool", native)
	_, _, err = ResolveBackend(m, "unknown__x")
	require.Error(t, err)
}

func TestJoinRejectsEmptyPrefix(t *testing.T) {
	_, err := Join("", "tool")
	require.ErrorIs(t, err, ErrInvalidPrefix)
}

func TestSplitRejectsMissingSeparator(t *testing.T) {
	_, _, err := Split("not_namespaced")
	require.Error(t, err)
}
