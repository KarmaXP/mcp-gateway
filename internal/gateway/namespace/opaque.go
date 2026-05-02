package namespace

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Opaque values (e.g. resource URIs) may contain the gateway separator "__".
// JoinOpaque encodes the native segment when needed so SplitOpaque recovers the original string.
const opaqueMarker = "gw0:"

// JoinOpaque builds prefix__native for multiplexed resources, prompts metadata, etc.
// Unlike Join, the native segment may contain "__"; it is wrapped in an opaque encoding when required.
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

// SplitOpaque reverses JoinOpaque (and plain Join for values without "__" inside the native part).
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
