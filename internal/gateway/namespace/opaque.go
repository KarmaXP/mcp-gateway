package namespace

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Opaque values (e.g. resource URIs) may contain the gateway separator "__".
// JoinOpaque encodes the native segment when needed so SplitOpaque recovers the original string.
const opaqueMarker = "gw0:"

// JoinOpaque encodes the native segment when it contains the namespace separator.
func JoinOpaque(prefix, native string) (string, error) {
	if native == "" {
		return "", fmt.Errorf("%w: empty native value", ErrInvalidToolName)
	}
	enc := native
	if strings.Contains(native, Separator) || strings.HasPrefix(native, opaqueMarker) {
		enc = opaqueMarker + base64.RawURLEncoding.EncodeToString([]byte(native))
	}
	return Join(prefix, enc)
}

func SplitOpaque(namespaced string) (prefix, native string, err error) {
	prefix, mid, err := Split(namespaced)
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(mid, opaqueMarker) {
		raw, derr := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(mid, opaqueMarker))
		if derr != nil {
			return "", "", fmt.Errorf("%w: decode opaque segment: %w", ErrInvalidToolName, derr)
		}
		return prefix, string(raw), nil
	}
	return prefix, mid, nil
}
