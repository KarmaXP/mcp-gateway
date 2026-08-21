// Tunable defaults for the gateway (no I/O).
package defaults

import "time"

const (
	VectorDimension = 384
	RouterTopK = 8
	RouterScoreMin = 0.35
	ReindexEmbedBatchSize = 64
	UpstreamMaxConcurrency        int64 = 8
	SessionOutboundChannelSize = 64
	SessionOutboundEnqueueTimeout = 2 * time.Second
	// SessionBroadcastMaxConcurrency caps concurrent EnqueueNotification calls per broadcast burst.
	SessionBroadcastMaxConcurrency = 32
	// SessionBroadcastWorkQueueSize bounds pending broadcast tasks before best-effort drop.
	SessionBroadcastWorkQueueSize = 256
	// SessionToolHistoryMax is the maximum successful tools/call names kept per SSE session for router context.
	SessionToolHistoryMax = 8

	MaxToolArgumentsJSONBytes = 256 << 10
	MaxToolArgumentsJSONDepth = 32
	MaxToolArgumentsJSONKeys = 256

	MaxMCPRPCBodyBytes = 1 << 20
	// MaxOTelSpanAttributeBytes caps string attribute values to limit export size / cardinality abuse.
	MaxOTelSpanAttributeBytes = 256
	MaxEmbedHTTPResponseBody = 1 << 22
	MaxQdrantHTTPBodyBytes = 8 << 20
	MaxQdrantErrorSnippetBytes = 200
	MaxQdrantPingDiscardBytes = 512
	MaxHTTPUpstreamErrorBody = 2048
	MaxSSEDiscardBodyBytes = 8 << 10

	DefaultVectorSearchTopK = 8
	MillisecondsPerSecond = 1000.0
)

var (
	DefaultEmbedServiceURL = "http://127.0.0.1:8001"
	DefaultQdrantHTTPURL = "http://127.0.0.1:6333"
	DefaultGatewayHTTPPort = "8080"

	DefaultQdrantCollectionName = "mcp_tool_catalog"
	DefaultTelemetryServiceName = "mcp-gateway"

	RouterEmbedTimeout = 10 * time.Second
	RouterQueryTimeout = 5 * time.Second

	PreflightQdrantTimeout = 3 * time.Second
	TelemetryShutdownTimeout = 8 * time.Second
	HTTPServerShutdownTimeout = 25 * time.Second
	HTTPReadHeaderTimeout = 10 * time.Second
	HTTPReadTimeout = 60 * time.Second
	HTTPIdleTimeout = 120 * time.Second
	UpstreamSSEHandshakeTimeout = 5 * time.Second
	MultiplexInitTimeout = 5 * time.Second
	MultiplexListTimeout = 10 * time.Second
	MultiplexCallTimeout = 60 * time.Second
	MultiplexListCacheTTL = 30 * time.Second
	QdrantHTTPClientTimeout = 60 * time.Second
	EmbedHTTPClientTimeout = 60 * time.Second
	EmbedTransportMaxIdleConns = 64
	EmbedTransportMaxIdleConnsPerHost = 16
	EmbedIdleConnTimeout = 90 * time.Second
	EmbedTLSHandshakeTimeout = 10 * time.Second
	EmbedExpectContinueTimeout = 1 * time.Second
	EmbedDialTimeout = 5 * time.Second
	EmbedTCPKeepAlive = 30 * time.Second

	DefaultJWKSCacheTTL = 5 * time.Minute
	JWKSStartupWarmupTimeout = 15 * time.Second

	OTLPMetricExportInterval = 15 * time.Second

	SSECommentHeartbeat = 30 * time.Second

	DefaultRateLimitRPS = 100
	DefaultRateLimitBurst = 200
	RateLimitBucketIdleTTL = 30 * time.Minute
)
