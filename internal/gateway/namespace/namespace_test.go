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

func TestJoinRejectsSeparatorInNativeName(t *testing.T) {
	_, err := Join("a", "foo__bar")
	require.ErrorIs(t, err, ErrNativeContainsSep)
}

func TestJoinNeverProducesANameSplitCannotRead(t *testing.T) {
	natives := []string{"foo__bar", "__lead", "trail__", "a__b__c"}
	for _, native := range natives {
		t.Run(native, func(t *testing.T) {
			joined, err := Join("a", native)
			if err != nil {
				require.ErrorIs(t, err, ErrNativeContainsSep)
				return
			}
			_, back, splitErr := Split(joined)
			require.NoError(t, splitErr,
				"Join published %q, which Split rejects, so the catalog would advertise an uncallable tool", joined)
			require.Equal(t, native, back)
		})
	}
}

func TestResolveUpstreamOrder(t *testing.T) {
	m := map[string]string{"z": "id-z", "a": "id-a"}
	bid, native, err := resolveUpstream(m, "z__tool")
	require.NoError(t, err)
	require.Equal(t, "id-z", bid)
	require.Equal(t, "tool", native)
	_, _, err = resolveUpstream(m, "unknown__x")
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
		prefix = "upstream0"
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
	const namespaced = "upstream0__search_documents"
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
