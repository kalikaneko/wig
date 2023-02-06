package collector

import (
	"os"
	"testing"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/model"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"git.autistici.org/ai3/attic/wig/gateway"
)

type testData struct {
	gateway.PeerStats
	t time.Time
}

func processStats(t *testing.T, sf *SessionFinder, testData []testData) []*model.Session {
	var out []*model.Session
	for _, td := range testData {
		sess := sf.Analyze(td.t, &td.PeerStats)
		if sess != nil {
			out = append(out, sess)
		}
	}
	return out
}

func TestSessionFinder(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.OpenDB(dir+"/sf.sql", datastore.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sf, _ := NewSessionFinder(db)

	t0 := time.Now()
	result := processStats(t, sf, []testData{
		{
			PeerStats: gateway.PeerStats{
				PublicKey:         "pk1",
				LastHandshakeTime: t0,
			},
			t: t0,
		},
		{
			PeerStats: gateway.PeerStats{
				PublicKey:         "pk1",
				LastHandshakeTime: t0.Add(5 * time.Minute),
			},
			t: t0.Add(10 * time.Minute),
		},
		{
			PeerStats: gateway.PeerStats{
				PublicKey:         "pk1",
				LastHandshakeTime: t0.Add(5 * time.Minute),
			},
			t: t0.Add(20 * time.Minute),
		},
	})

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got: %+v", result)
	}
	if n := len(sf.ActiveSessions()); n > 0 {
		t.Fatalf("there are %d active sessions, expected 0", n)
	}
}
