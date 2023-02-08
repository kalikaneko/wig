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

type Credentials interface {
	Identity() string
	Roles() []string
}

type Authn interface {
	CredentialsFromRequest(*http.Request) (Credentials, error)
}

type Authz interface {
	HasPermission(Credentials, string) bool
}

type rbac struct {
	rules map[string][]string
}

func NewRBAC(permissions map[string][]string) Authz {
	return &rbac{rules: permissions}
}

func (a *rbac) targetsForRole(role string) []string {
	return a.rules[role]
}

func (a *rbac) HasPermission(creds Credentials, target string) bool {
	for _, role := range creds.Roles() {
		for _, t := range a.targetsForRole(role) {
			if t == target {
				return true
			}
		}
	}
	return false
}

type nilAuthz struct{}

func (_ nilAuthz) HasPermission(_ Credentials, _ string) bool { return true }

func NilAuthz() Authz {
	return new(nilAuthz)
}

type nilAuthn struct{}

func (_ nilAuthn) CredentialsFromRequest(_ *http.Request) (Credentials, error) {
	return nil, nil
}

func NilAuthn() Authn {
	return new(nilAuthn)
}
