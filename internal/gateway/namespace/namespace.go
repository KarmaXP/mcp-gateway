package namespace

import (
	"errors"
	"fmt"
	"strings"
)

const Separator = "__"

var (
	ErrInvalidPrefix = errors.New("namespace: invalid prefix")
	ErrInvalidToolName = errors.New("namespace: invalid namespaced tool name")
	ErrAmbiguousPrefix = errors.New("namespace: prefix is not unique in catalog")
	ErrNativeContainsSep = errors.New("namespace: native tool name must not contain separator")
)

func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("%w: empty", ErrInvalidPrefix)
	}
	if strings.Contains(prefix, Separator) {
		return fmt.Errorf("%w: must not contain %q", ErrInvalidPrefix, Separator)
	}
	return nil
}

func Join(prefix, native string) (string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return "", err
	}
	if native == "" {
		return "", fmt.Errorf("%w: empty native name", ErrInvalidToolName)
	}
	if strings.Contains(native, Separator) {
		return "", ErrNativeContainsSep
	}
	return prefix + Separator + native, nil
}

func Split(namespaced string) (prefix, native string, err error) {
	if namespaced == "" {
		return "", "", fmt.Errorf("%w: empty", ErrInvalidToolName)
	}
	idx := strings.Index(namespaced, Separator)
	if idx <= 0 || idx+len(Separator) >= len(namespaced) {
		return "", "", fmt.Errorf("%w: missing %q separator", ErrInvalidToolName, Separator)
	}
	prefix = namespaced[:idx]
	native = namespaced[idx+len(Separator):]
	if err := ValidatePrefix(prefix); err != nil {
		return "", "", err
	}
	if native == "" {
		return "", "", fmt.Errorf("%w: empty native segment", ErrInvalidToolName)
	}
	if strings.Contains(native, Separator) {
		return "", "", ErrNativeContainsSep
	}
	return prefix, native, nil
}

func ResolveBackend(prefixToBackend map[string]string, namespaced string) (backendID, nativeName string, err error) {
	prefix, native, err := Split(namespaced)
	if err != nil {
		return "", "", err
	}
	bid, ok := prefixToBackend[prefix]
	if !ok {
		return "", "", fmt.Errorf("%w: unknown prefix %q", ErrInvalidToolName, prefix)
	}
	return bid, native, nil
}
