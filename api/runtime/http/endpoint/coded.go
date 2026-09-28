package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// CodedError is a handler error that carries a stable, client-facing code,
// a message meant for logs rather than display, and optional per-field
// detail codes. Wrap it in HttpError to choose its status.
type CodedError struct {
	Code    string
	Message string
	Details map[string]string
}

// Error satisfies the error interface.
func (e CodedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type codedBody struct {
	Error codedBodyError `json:"error"`
}

type codedBodyError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// CodedJSONWriter renders successful outputs like DefaultJSONWriter, and
// every error as {"error":{"code","message","details"}}.
//
// The status comes from an HttpError in the error chain (500 if none). The
// code, message and details come from a CodedError in the chain. Any other
// error gets a code derived from the status text (e.g. "BAD_REQUEST"); for
// a 5xx its text is logged but never sent, so internal details don't leak.
func CodedJSONWriter(ctx context.Context, w http.ResponseWriter, r *http.Request, output any, handlerErr error) error {
	if handlerErr == nil {
		return DefaultJSONWriter(ctx, w, r, output, nil)
	}

	status := http.StatusInternalServerError
	var httpErr HttpError
	if errors.As(handlerErr, &httpErr) {
		status = httpErr.StatusCode
	}

	var body codedBody
	var coded CodedError
	switch {
	case errors.As(handlerErr, &coded):
		body.Error = codedBodyError{Code: coded.Code, Message: coded.Message, Details: coded.Details}
	case status >= http.StatusInternalServerError:
		slog.ErrorContext(ctx, "request failed", "method", r.Method, "path", r.URL.Path, "err", handlerErr)
		body.Error = codedBodyError{Code: statusCode(status), Message: http.StatusText(status)}
	default:
		body.Error = codedBodyError{Code: statusCode(status), Message: handlerErr.Error()}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(payload)
	return err
}

// statusCode turns a status into an upper-snake code, e.g. 404 -> "NOT_FOUND".
func statusCode(status int) string {
	return strings.ToUpper(strings.ReplaceAll(http.StatusText(status), " ", "_"))
}
