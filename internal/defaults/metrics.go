package defaults

// OpenTelemetry metric attribute values — bounded cardinality (O5); no free-text or IDs.

const (
	MetricPolicyOutcomeAllow = "allow"
	MetricPolicyOutcomeDeny  = "deny"

	MetricPolicyReasonAllowListMatch   = "allow_list_match"
	MetricPolicyReasonNotInAllowList   = "not_in_allow_list"
	MetricPolicyReasonPolicyEvalFailed = "policy_eval_failed"
	MetricPolicyReasonOther            = "other"
)

const (
	MetricJWKSResultHit             = "hit"
	MetricJWKSResultRefresh         = "refresh"
	MetricJWKSResultErrorFetch      = "error_fetch"
	MetricJWKSResultErrorMissingKid = "error_missing_kid"
	MetricJWKSResultErrorUnknownKid = "error_unknown_kid"
)

const (
	MetricRateLimitAllowed   = "allowed"
	MetricRateLimitThrottled = "throttled"
)

const (
	MetricArgsStageLimits = "limits"
	MetricArgsStageSchema = "schema"
)

const (
	MetricArgsResultPass = "pass"
	MetricArgsResultFail = "fail"
)
