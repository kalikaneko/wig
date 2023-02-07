package httpapi

import "net/http"

type Builder interface {
	BuildAPI(*API)
}

type BuilderFunc func(*API)

func (f BuilderFunc) BuildAPI(api *API) { f(api) }

type method struct {
	path    string
	handler http.Handler
}

type API struct {
	*http.ServeMux

	authn Authn
	authz Authz
}

func New(authn Authn, authz Authz) *API {
	return &API{
		ServeMux: http.NewServeMux(),
		authn:    authn,
		authz:    authz,
	}
}

func (a *API) Add(b Builder) {
	b.BuildAPI(a)
}

func (a *API) WithAuth(target string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Authn.
		creds, err := a.authn.CredentialsFromRequest(req)
		if err != nil {
			http.Error(w, "Unauthenticated", http.StatusUnauthorized)
			return
		}

		// Authz.
		if !a.authz.HasPermission(creds, target) {
			http.Error(w, "Unauthorized", http.StatusForbidden)
			return
		}

		h.ServeHTTP(w, req)
	})
}

func (a *API) Handle(path string, h http.Handler) {
	a.ServeMux.Handle(path, h)
}
