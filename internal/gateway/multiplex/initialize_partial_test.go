package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type emptyInitializeUpstream struct {
	*mock.MockUpstream
}

func (u *emptyInitializeUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "initialize" {
		return rpc.NewResult(req.ID, nil), nil
	}
	return u.MockUpstream.Call(ctx, req)
}

func TestInitializeReportsNoPartialFailureWhenOthersSucceeded(t *testing.T) {
	healthy := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	quiet := &emptyInitializeUpstream{MockUpstream: mock.NewMockUpstream("b2", "beta", []string{"ping"})}
	a, err := New(context.Background(), []upstream.Client{healthy, quiet},
		WithListTTL(0),
		WithReportPartialFailures(true),
	)
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var body struct {
		ServerInfo struct {
			Extras map[string]any `json:"extras"`
		} `json:"serverInfo"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	require.NotContains(t, body.ServerInfo.Extras, "partial_failures",
		"an upstream that answered initialize with an empty result is not a partial failure while others succeeded")
}
