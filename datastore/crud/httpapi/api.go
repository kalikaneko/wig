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
}

func New() *API {
	return &API{http.NewServeMux()}
}

func (a *API) Add(b Builder) {
	b.BuildAPI(a)
}

func (a *API) Handle(path string, h http.Handler) {
	a.ServeMux.Handle(path, h)
}
