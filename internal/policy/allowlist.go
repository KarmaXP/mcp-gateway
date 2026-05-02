package policy

import "fmt"

// allowed empty ⇒ any tool in catalog (no restriction).
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
