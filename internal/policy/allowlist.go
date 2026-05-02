package policy

import "fmt"

// AllowedListContains reports whether namespacedTool is permitted by the effective allow list.
// An empty allowed list means no JWT/RAR restriction (caller may use the full merged catalog).
func AllowedListContains(namespacedTool string, allowed []string) (bool, error) {
	if len(allowed) == 0 {
		return true, nil
	}
	for _, e := range allowed {
		ok, err := MatchTool(namespacedTool, e)
		if err != nil {
			return false, fmt.Errorf("policy allow list: %w", err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
