package mode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in    string
		want  Mode
		valid bool
	}{
		{in: "off", want: Off, valid: true},
		{in: "on", want: On, valid: true},
		{in: "assist_list", want: AssistList, valid: true},
		{in: "filter_list", want: FilterList, valid: true},
		{in: "  ON  ", want: On, valid: true},
		{in: "AssistList", want: Off, valid: false},
		{in: "enabled", want: Off, valid: false},
		{in: "", want: Off, valid: false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := Parse(tc.in)
			require.Equal(t, tc.valid, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestActive(t *testing.T) {
	t.Parallel()
	require.False(t, Off.Active())
	require.True(t, On.Active())
	require.True(t, AssistList.Active())
	require.True(t, FilterList.Active())
	require.False(t, Mode("nonsense").Active(), "an unknown mode must not read as active")
}
