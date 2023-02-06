package model

import "time"

type Session struct {
	PeerPublicKey string    `json:"peer_public_key" db:"peer_public_key"`
	SrcASNum      string    `json:"src_as_num" db:"src_as_num"`
	SrcASOrg      string    `json:"src_as_org" db:"src_as_org"`
	SrcCountry    string    `json:"src_country" db:"src_country"`
	Begin         time.Time `json:"begin" db:"begin_timestamp"`
	End           time.Time `json:"end" db:"end_timestamp"`
	Active        bool      `json:"active" db:"active"`
}
