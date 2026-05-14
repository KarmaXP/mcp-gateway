package mcpwire

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsCatalogListChangedNotification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		want   bool
	}{
		{name: "tools notifications", method: NotificationToolsListChanged, want: true},
		{name: "tools legacy", method: LegacyToolsListChanged, want: true},
		{name: "resources notifications", method: NotificationResourcesListChanged, want: true},
		{name: "resources legacy", method: LegacyResourcesListChanged, want: true},
		{name: "prompts notifications", method: NotificationPromptsListChanged, want: true},
		{name: "prompts legacy", method: LegacyPromptsListChanged, want: true},
		{name: "other method", method: "notifications/tools/call", want: false},
		{name: "empty", method: "", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsCatalogListChangedNotification(tc.method))
		})
	}
}

func TestIsToolsListChangedNotificationSubsetOfCatalogMatcher(t *testing.T) {
	t.Parallel()
	require.True(t, IsToolsListChangedNotification(NotificationToolsListChanged))
	require.True(t, IsCatalogListChangedNotification(NotificationToolsListChanged))
	require.True(t, IsToolsListChangedNotification(LegacyToolsListChanged))
	require.True(t, IsCatalogListChangedNotification(LegacyToolsListChanged))
	require.False(t, IsToolsListChangedNotification(NotificationResourcesListChanged))
	require.False(t, IsToolsListChangedNotification(NotificationPromptsListChanged))
}
