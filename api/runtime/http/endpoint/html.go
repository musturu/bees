package endpoint

import (
	"context"
	"errors"
	"html/template"
	"net/http"
)

// HTMLResponse is an Endpoint output rendered via html/template. Partial, if
// set, is used instead of Template when the request carries an HX-Request
// header (htmx boosted/partial requests) -- letting one handler serve both a
// full page and its htmx fragment.
type HTMLResponse struct {
	Template string
	Partial  string
	Data     any
}

// HTMLWriter returns a WriteFunc that renders HTMLResponse output through tmpl.
func HTMLWriter(tmpl *template.Template) WriteFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, output any, handlerErr error) error {
		if handlerErr != nil {
			status := http.StatusInternalServerError
			var httpErr HttpError
			if errors.As(handlerErr, &httpErr) {
				status = httpErr.StatusCode
			}
			http.Error(w, handlerErr.Error(), status)
			return nil
		}
		resp, ok := output.(HTMLResponse)
		if !ok {
			return errors.New("endpoint: HTMLWriter: output is not endpoint.HTMLResponse")
		}
		name := resp.Template
		if resp.Partial != "" && r.Header.Get("HX-Request") == "true" {
			name = resp.Partial
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		return tmpl.ExecuteTemplate(w, name, resp.Data)
	}
}
