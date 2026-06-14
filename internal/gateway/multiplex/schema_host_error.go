package multiplex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Never include instance values in the returned string (policy audit rules).
func hostVisibleJSONSchemaError(err error) string {
	var verr *jsonschema.ValidationError
	if errors.As(err, &verr) {
		return hostMessageFromValidationError(firstValidationLeaf(verr))
	}
	return "arguments do not match tool schema"
}

func firstValidationLeaf(e *jsonschema.ValidationError) *jsonschema.ValidationError {
	if e == nil {
		return nil
	}
	if len(e.Causes) == 0 {
		return e
	}
	for _, c := range e.Causes {
		if leaf := firstValidationLeaf(c); leaf != nil {
			return leaf
		}
	}
	return e
}

func hostMessageFromValidationError(e *jsonschema.ValidationError) string {
	if e == nil {
		return "arguments do not match tool schema"
	}
	loc := "/" + strings.Join(e.InstanceLocation, "/")
	var kw string
	if e.ErrorKind != nil {
		kw = strings.Join(e.ErrorKind.KeywordPath(), "/")
	}
	if kw != "" {
		return fmt.Sprintf("schema validation failed at %s (constraint %s)", loc, kw)
	}
	return fmt.Sprintf("schema validation failed at %s", loc)
}
