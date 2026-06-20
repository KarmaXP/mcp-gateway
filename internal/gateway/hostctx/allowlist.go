package hostctx

// PolicyAllowListView maps hostctx allow-list mode to policy.AllowedListContains semantics.
// unrestricted -> nil slice; deny-all -> non-nil empty; restricted -> names copy.
func PolicyAllowListView(mode AllowListMode, names []string) []string {
	switch mode {
	case AllowListDenyAll:
		return []string{}
	case AllowListRestricted:
		return names
	default:
		return nil
	}
}
