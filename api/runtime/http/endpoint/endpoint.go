package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Binder can be provided to populate an input from an *http.Request.
// If nil, no binding is performed.
type Binder[T any] func(r *http.Request, in *T) error

// WriteFunc is invoked after the handler finishes.
// It receives both the handler output and any error returned by the handler.
type WriteFunc func(ctx context.Context, w http.ResponseWriter, output any, handlerErr error) error

// HttpError wraps an error with an explicit status code.
type HttpError struct {
	StatusCode int
	Err        error
}

// Error satisfies the error interface.
// If Err is nil, the status text is used as the message.
func (e HttpError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return http.StatusText(e.StatusCode)
}

// Unwrap exposes the underlying error for errors.As support.
func (e HttpError) Unwrap() error {
	return e.Err
}

// DefaultBinder decodes JSON request body into the provided input value.
// It returns nil if there is no body.
func DefaultBinder[T any](r *http.Request, in *T) error {
	if r == nil || r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	return dec.Decode(in)
}

// DefaultJSONWriter serializes successful outputs and handler errors into JSON.
// Errors populate an “error“ field, and handler errors receive their status
// code if they implement HttpError. Non-HttpError failures become 500 responses.
func DefaultJSONWriter(ctx context.Context, w http.ResponseWriter, output any, handlerErr error) error {
	w.Header().Set("Content-Type", "application/json")
	if handlerErr != nil {
		status := http.StatusInternalServerError
		var httpErr HttpError
		if errors.As(handlerErr, &httpErr) {
			status = httpErr.StatusCode
			if httpErr.Err != nil {
				handlerErr = httpErr.Err
			}
		}
		payload, err := json.Marshal(map[string]string{"error": handlerErr.Error()})
		if err != nil {
			return err
		}
		w.WriteHeader(status)
		_, err = w.Write(payload)
		return err
	}
	if output == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	payload, err := json.Marshal(output)
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(payload)
	return err
}

// Endpoint is a typed endpoint where T is the input type and R is the output type.
// The handler receives *T so a single input struct can be reused across endpoints.
type Endpoint[T any, R any] struct {
	MethodPattern string
	Bind          Binder[T]                                   // optional
	Handle        func(ctx context.Context, in *T) (R, error) // required
	Write         WriteFunc                                   // optional
}

// NewEndpoint constructs a typed Endpoint for input type T and output type R.
func NewEndpoint[T any, R any](methodPattern string, handle func(ctx context.Context, in *T) (R, error)) *Endpoint[T, R] {
	return &Endpoint[T, R]{
		MethodPattern: methodPattern,
		Handle:        handle,
	}
}

// NewJSONEndpoint constructs a typed Endpoint using the default JSON binder.
func NewJSONEndpoint[T any, R any](methodPattern string, handle func(ctx context.Context, in *T) (R, error)) *Endpoint[T, R] {
	ep := NewEndpoint(methodPattern, handle)
	ep.Bind = DefaultBinder[T]
	return ep
}

// Handler returns an http.HandlerFunc that runs bind → business logic → write.
func (e *Endpoint[T, R]) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in := new(T)
		if e.Bind != nil {
			if err := e.Bind(r, in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if err := r.Body.Close(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if e.Handle == nil {
			http.Error(w, "endpoint handle is nil", http.StatusBadRequest)
			return
		}
		out, handleErr := e.Handle(r.Context(), in)
		writer := e.Write
		if writer == nil {
			writer = DefaultJSONWriter
		}
		// pass typed output as any to keep writer API unchanged
		if err := writer(r.Context(), w, any(out), handleErr); err != nil {
			status := http.StatusInternalServerError
			var httpErr HttpError
			if errors.As(err, &httpErr) {
				status = httpErr.StatusCode
			}
			http.Error(w, err.Error(), status)
		}
	}
}
