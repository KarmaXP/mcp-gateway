package namespace

import "testing"

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
