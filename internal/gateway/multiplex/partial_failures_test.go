package multiplex

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyCallFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "timeout", err: context.DeadlineExceeded, want: PartialFailureTimeout},
		{name: "transport", err: errors.New("connection reset"), want: PartialFailureTransport},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyCallFailure(tc.err))
		})
	}
}
