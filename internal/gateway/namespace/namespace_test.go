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

func BenchmarkNamespaceAdd(b *testing.B) {
	const (
		prefix = "backend0"
		native = "search_documents"
	)
	b.ReportAllocs()
	b.SetBytes(int64(len(prefix) + len(Separator) + len(native)))

	for b.Loop() {
		ns, err := Join(prefix, native)
		if err != nil {
			b.Fatal(err)
		}
		if ns == "" {
			b.Fatal("empty namespaced tool")
		}
	}
}

func BenchmarkNamespaceStrip(b *testing.B) {
	const namespaced = "backend0__search_documents"
	b.ReportAllocs()
	b.SetBytes(int64(len(namespaced)))

	for b.Loop() {
		prefix, native, err := Split(namespaced)
		if err != nil {
			b.Fatal(err)
		}
		if prefix == "" || native == "" {
			b.Fatal("empty split segment")
		}
	}
}
