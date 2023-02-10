package registration

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httpapi"
	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
	"github.com/yl2chen/cidranger"
)

const apiURLRegisterPeer = "/api/v1/register-peer"

type RegistrationAPI struct {
	db *sqlx.DB
	mx sync.Mutex
}

func NewRegistrationAPI(db *sqlx.DB) *RegistrationAPI {
	return &RegistrationAPI{
		db: db,
	}
}

func (r *RegistrationAPI) RegisterNewPeer(intfName, publicKey string, ttl time.Duration) (*model.Peer, error) {
	// The SQL transaction can't protect us against all types of
	// conflict: while it may detect conflicting same-IP-range
	// assignment, it won't be able to spot overlapping ranges
	// etc. because IP ranges are just strings to it. So, we're
	// using a mutex.
	//
	// Also, this function is *ridiculously* expensive, that's the
	// price of allowing arbitrary IP range allocation to peers.
	// But this is not a DHCP server, the IP address allocation
	// only happens at peer registration time.
	//
	r.mx.Lock()
	defer r.mx.Unlock()

	peer := &model.Peer{
		PublicKey: publicKey,
		Interface: intfName,
	}
	if ttl > 0 {
		peer.Expire = time.Now().Add(ttl)
	}

	err := sqlite.WithTx(r.db, func(tx *sqlx.Tx) error {
		var intf model.Interface
		if err := tx.QueryRowx("SELECT * FROM interfaces WHERE name = ?", intfName).StructScan(&intf); err != nil {
			return err
		}

		allocated, err := r.allocatedRanges(tx, intfName)
		if err != nil {
			return err
		}

		// Assign IPv4 address.
		if !intf.IP.IsNil() {
			ip, err := r.nextFreeIP(tx, intf.IP, allocated)
			if err != nil {
				return err
			}
			peer.IP = model.NewCIDR(ip, 32)
		}

		// Assign IPv6 address.
		if !intf.IP6.IsNil() {
			ip, err := r.nextFreeIP(tx, intf.IP6, allocated)
			if err != nil {
				return err
			}
			peer.IP = model.NewCIDR(ip, 128)
		}

		return nil
	})

	return peer, err
}

func (r *RegistrationAPI) nextFreeIP(tx *sqlx.Tx, ipnet *model.CIDR, allocated cidranger.Ranger) (net.IP, error) {
	g := newGeneratorFromIPNet(ipnet.IPNet)

	for {
		ip := g.Next()
		if ip == nil {
			return nil, errors.New("pool exhausted")
		}
		taken, _ := allocated.Contains(ip)
		if !taken {
			return ip, nil
		}
	}
}

func (r *RegistrationAPI) allocatedRanges(tx *sqlx.Tx, intfName string) (cidranger.Ranger, error) {
	rows, err := tx.Queryx("SELECT ip, ip6 FROM peer WHERE interface = ?", intfName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ranger := cidranger.NewPCTrieRanger()

	for rows.Next() {
		var ip, ip6 model.CIDR
		if err := rows.Scan(&ip, &ip6); err != nil {
			return nil, err
		}
		if !ip.IsNil() {
			ranger.Insert(cidranger.NewBasicRangerEntry(ip.IPNet))
		}
		if !ip6.IsNil() {
			ranger.Insert(cidranger.NewBasicRangerEntry(ip6.IPNet))
		}
	}

	return ranger, rows.Err()
}

type RegisterPeerRequest struct {
	Interface string `json:"interface"`
	PublicKey string `json:"public_key"`
	TTL       int    `json:"ttl"`
}

func (r *RegistrationAPI) handleRegisterPeer(w http.ResponseWriter, req *http.Request) {
	var rr RegisterPeerRequest
	httptransport.ServeJSON(w, req, &rr, func() (interface{}, error) {
		return r.RegisterNewPeer(rr.Interface, rr.PublicKey, time.Second*time.Duration(rr.TTL))
	})
}

func (r *RegistrationAPI) BuildAPI(api *httpapi.API) {
	api.Handle(apiURLRegisterPeer, api.WithAuth(
		"register-peer", http.HandlerFunc(r.handleRegisterPeer)))
}
