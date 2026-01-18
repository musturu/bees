package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
)

// Binder can be provided to populate an input from an *http.Request.
// If nil, no binding is performed.
type Binder[T any] func(r *http.Request, in *T) error

// Output writes the response. It may access Input state through captured references.
type Output interface {
	Write(ctx context.Context, w http.ResponseWriter) error
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

// Endpoint is a typed endpoint where T is the input struct type.
// The handler receives *T so a single input struct can be reused across endpoints.
type Endpoint[T any] struct {
	MethodPattern string
	Bind          Binder[T]                                        // optional
	Handle        func(ctx context.Context, in *T) (Output, error) // required
}

// NewEndpoint constructs a typed Endpoint for input type T.
func NewEndpoint[T any](methodPattern string, handle func(ctx context.Context, in *T) (Output, error)) *Endpoint[T] {
	return &Endpoint[T]{
		MethodPattern: methodPattern,
		Handle:        handle,
	}
}

// NewJSONEndpoint constructs a typed Endpoint using the default JSON binder.
func NewJSONEndpoint[T any](methodPattern string, handle func(ctx context.Context, in *T) (Output, error)) *Endpoint[T] {
	ep := NewEndpoint(methodPattern, handle)
	ep.Bind = DefaultBinder[T]
	return ep
}

// Handler returns an http.HandlerFunc that runs bind → business logic → write.
// On any error it responds with 400 and the error string.
func (e *Endpoint[T]) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in := new(T)
		if e.Bind != nil {
			if err := e.Bind(r, in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if e.Handle == nil {
			http.Error(w, "endpoint handle is nil", http.StatusBadRequest)
			return
		}
		out, err := e.Handle(r.Context(), in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if out == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := out.Write(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
}
