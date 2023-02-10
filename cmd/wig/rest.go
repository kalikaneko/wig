package main

import (
	"git.autistici.org/ai3/tools/wig/datastore/crud"
	"git.autistici.org/ai3/tools/wig/datastore/model"
)

var apiURLBase = "/api/v1"

func init() {
	crud.RegisterCommands(
		model.Model,
		model.PeerType,
		apiURLBase,
	)
	crud.RegisterCommands(
		model.Model,
		model.InterfaceType,
		apiURLBase,
	)
	crud.RegisterCommands(
		model.Model,
		model.TokenType,
		apiURLBase,
	)
}
