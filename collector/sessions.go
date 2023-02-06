package collector

import (
	"log"
	"sync"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/datastore/sessiondb"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"git.autistici.org/ai3/attic/wig/gateway"
	"github.com/jmoiron/sqlx"
)

var (
	sessionInactivityTimeout = 600 * time.Second
	sessionDumpInterval      = 300 * time.Second
)

// SessionFinder performs 'edge detection' on the incoming stats logs
// to detect begin / end session times and generate complete Session
// entries.
//
// Peers can appear on different gateways, so we need to keep track of
// the most recently seen LastHandshakeTime for each peer to build a
// linear history of events.
//
// Due to the large number of lookups expected, the SessionFinder
// maintains its state in memory, and periodically (every few minutes)
// dumps it to the database for persistence.
//
type SessionFinder struct {
	refiner *ipRefiner
	done    chan struct{}
	wg      sync.WaitGroup

	mx             sync.Mutex
	activeSessions map[string]*model.Session
	lastHandshake  map[string]time.Time
}

func NewSessionFinder(db *sqlx.DB) (*SessionFinder, error) {
	refiner, err := newIPRefiner(nil)
	if err != nil {
		return nil, err
	}
	sf := SessionFinder{
		done:    make(chan struct{}),
		refiner: refiner,
	}

	// nolint: errcheck
	sessiondb.WithTx(db, func(tx sessiondb.Tx) error {
		sf.activeSessions = tx.GetActiveSessions()
		sf.lastHandshake = tx.GetLastHandshakeTimes()
		return sqlite.ErrRollback
	})

	sf.wg.Add(1)
	go sf.dumper(db)

	return &sf, nil
}

func (f *SessionFinder) Close() {
	close(f.done)
	f.wg.Wait()
}

func (f *SessionFinder) dumper(db *sqlx.DB) {
	defer f.wg.Done()

	tick := time.NewTicker(sessionDumpInterval)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			if err := sessiondb.WithTx(db, func(tx sessiondb.Tx) error {
				return tx.DumpActiveSessions(f.ActiveSessions())
			}); err != nil {
				log.Printf("data dump failed: %v", err)
			}
		case <-f.done:
			return
		}
	}
}

func (f *SessionFinder) ActiveSessions() []*model.Session {
	f.mx.Lock()
	defer f.mx.Unlock()

	out := make([]*model.Session, 0, len(f.activeSessions))
	for _, s := range f.activeSessions {
		out = append(out, s)
	}
	return out
}

func (f *SessionFinder) ActiveSessionByPublicKey(pkey string) *model.Session {
	f.mx.Lock()
	defer f.mx.Unlock()
	return f.activeSessions[pkey]
}

func (f *SessionFinder) setHandshakeTime(pkey string, ht time.Time) time.Time {
	if last, ok := f.lastHandshake[pkey]; ok && ht.Before(last) {
		return last
	}
	f.lastHandshake[pkey] = ht
	return ht
}

func (f *SessionFinder) Analyze(now time.Time, s *gateway.PeerStats) *model.Session {
	f.mx.Lock()
	defer f.mx.Unlock()

	ht := f.setHandshakeTime(s.PublicKey, s.LastHandshakeTime)

	active := now.Add(-sessionInactivityTimeout).Before(ht)

	cur, ok := f.activeSessions[s.PublicKey]
	switch {
	case !ok && active:
		// "Up" edge, a new session.
		cur = &model.Session{
			PeerPublicKey: s.PublicKey,
			Begin:         ht,
			Active:        true,
		}
		f.activeSessions[s.PublicKey] = cur
	case ok && !active:
		// "Down" edge, a session has become stale (inactive).
		delete(f.activeSessions, s.PublicKey)
		cur.End = ht
		cur.Active = false
		// Augment the Session with IP-related data, without
		// storing the IP anywhere (it's no longer available
		// after this point).
		f.refiner.addIPInfo(cur, s.Endpoint)
		return cur
	}
	return nil
}
