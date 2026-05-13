package backend

import "github.com/KarmaXP/mcp-gateway/internal/rpc"

type NotificationReceiver interface {
	SetOnNotification(func(*rpc.Request))
}

func RegisterNotificationHandlers(upstreams []Upstream, fn func(*rpc.Request)) {
	if fn == nil {
		return
	}
	for _, u := range upstreams {
		if nr, ok := u.(NotificationReceiver); ok {
			nr.SetOnNotification(fn)
		}
	}
}
