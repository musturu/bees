package auth

import (
	"net/http"
	"net/url"
	"strings"

	"bees/api/runtime/http/middleware"
)

// RequireSession returns middleware that rejects requests with no valid
// session cookie. It's the whole story for a service with no other
// credential type; a service that also accepts e.g. a personal access
// token (mustgit does, for git-over-HTTP) calls cfg.SessionFromRequest
// directly instead, so it can fall back to its own credential check before
// giving up.
//
// A browser navigation (GET, Accept: text/html, not an htmx fragment
// request) is redirected to /auth/login; anything else -- API calls, git
// clients, htmx fragments -- gets a bare 401, since those callers can't
// follow a redirect into an HTML login page.
func RequireSession(cfg CookieConfig) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, err := cfg.SessionFromRequest(r)
			if err != nil {
				DenyOrRedirect(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithSession(r.Context(), sess)))
		})
	}
}

// DenyOrRedirect is the shared "no valid credential" response: a redirect
// to /auth/login for a browser navigation, a 401 otherwise. Exported so a
// service with its own combined middleware (session cookie + some other
// credential type) can reuse the same policy after its own checks fail too.
func DenyOrRedirect(w http.ResponseWriter, r *http.Request) {
	if wantsLoginRedirect(r) {
		returnTo := r.URL.RequestURI()
		http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(returnTo), http.StatusSeeOther)
		return
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="must"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func wantsLoginRedirect(r *http.Request) bool {
	if r.Header.Get("HX-Request") == "true" {
		return false
	}
	if r.Method != http.MethodGet {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
