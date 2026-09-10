package endpoint

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Stream is an Endpoint output that pipes a live io.Reader back as the
// response body instead of marshaling a materialized value. Useful for
// proxying a subprocess's stdout, a large file, or any other data whose
// full size shouldn't be buffered in memory.
type Stream struct {
	ContentType string
	Body        io.ReadCloser
}

// StreamWriter is a WriteFunc that copies a Stream's Body to the response,
// closing it when done. handlerErr falls back to DefaultJSONWriter.
func StreamWriter(ctx context.Context, w http.ResponseWriter, r *http.Request, output any, handlerErr error) error {
	if handlerErr != nil {
		return DefaultJSONWriter(ctx, w, r, nil, handlerErr)
	}
	s, ok := output.(Stream)
	if !ok {
		return fmt.Errorf("endpoint: StreamWriter: output is not endpoint.Stream (%T)", output)
	}
	defer s.Body.Close()
	if s.ContentType != "" {
		w.Header().Set("Content-Type", s.ContentType)
	}
	_, err := io.Copy(w, s.Body)
	return err
}
