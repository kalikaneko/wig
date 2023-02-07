package model

import "git.autistici.org/ai3/attic/wig/datastore/crud"

var Model *crud.Model

func init() {
	Model = crud.New()
	Model.Register(PeerType)
	Model.Register(InterfaceType)
	Model.Register(TokenType)
}
