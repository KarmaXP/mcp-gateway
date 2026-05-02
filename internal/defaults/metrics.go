package defaults

// OTel metric label values (bounded cardinality; O5).

const (
	MetricPolicyOutcomeAllow = "allow"
	MetricPolicyOutcomeDeny = "deny"

	MetricPolicyReasonAllowListMatch = "allow_list_match"
	MetricPolicyReasonNotInAllowList = "not_in_allow_list"
	MetricPolicyReasonPolicyEvalFailed = "policy_eval_failed"
	MetricPolicyReasonOther = "other"

	MetricJWKSResultHit = "hit"
	MetricJWKSResultRefresh = "refresh"
	MetricJWKSResultErrorFetch = "error_fetch"
	MetricJWKSResultErrorMissingKid = "error_missing_kid"
	MetricJWKSResultErrorUnknownKid = "error_unknown_kid"

	MetricRateLimitAllowed = "allowed"
	MetricRateLimitThrottled = "throttled"

	MetricArgsStageLimits = "limits"
	MetricArgsStageSchema = "schema"

	MetricArgsResultPass = "pass"
	MetricArgsResultFail = "fail"

	MetricBytesRejectReasonHTTPBody = "http_body_too_large"
	MetricBytesRejectReasonToolArgs = "tool_args_too_large"
)
