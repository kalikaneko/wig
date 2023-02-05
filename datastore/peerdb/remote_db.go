package peerdb

import (
	"net/http"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/rest"
)

type peerRESTHandler struct {
	api DatabaseAPI
}

func (p *peerRESTHandler) Name() string { return "peer" }

func (p *peerRESTHandler) NewInstance() interface{} {
	return new(datastore.Peer)
}

func (p *peerRESTHandler) Add(obj interface{}) error {
	return p.api.Add(obj.(*datastore.Peer))
}

func (p *peerRESTHandler) Update(obj interface{}) error {
	return p.api.Update(obj.(*datastore.Peer))
}

func (p *peerRESTHandler) Delete(id string) error {
	return p.api.Delete(&datastore.Peer{PublicKey: id})
}

func (p *peerRESTHandler) Find(id string) (interface{}, bool) {
	return p.api.FindByPublicKey(id)
}

func NewPeerAPIHandler(api DatabaseAPI, h http.Handler) http.Handler {
	return rest.NewHandler(&peerRESTHandler{api}, h)
}
