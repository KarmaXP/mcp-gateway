package policy

import "fmt"

// allowed nil ⇒ any tool in catalog (no restriction); non-nil empty ⇒ deny-all.
func AllowedListContains(namespacedTool string, allowed []string) (bool, error) {
	if allowed == nil {
		return true, nil
	}
	if len(allowed) == 0 {
		return false, nil
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
