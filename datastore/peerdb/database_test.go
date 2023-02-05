package peerdb

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/jmoiron/sqlx"
)

func TestSequencer_Mem(t *testing.T) {
	runSequencerTest(t, newMemSequencer(Sequence(1)))
}

func TestSequencer_SQL(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, err := sqlite.OpenDB(dir+"/db1.sql", datastore.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer sql1.Close()

	// nolint: errcheck
	sqlite.WithTx(sql1, func(tx *sqlx.Tx) error {
		runSequencerTest(t, newSQLSequencer(tx))
		return sqlite.ErrRollback
	})
}

func runSequencerTest(t *testing.T, s sequencer) {
	for _, start := range []int{1, 100} {
		if err := s.SetSequence(Sequence(start)); err != nil {
			t.Fatalf("SetSequence() error: %v", err)
		}

		getVals := make([]Sequence, 10)
		incVals := make([]Sequence, 10)
		for i := 0; i < 10; i++ {
			getVals[i] = s.GetSequence()
			incVals[i] = s.Inc()
		}

		for i := 0; i < 10; i++ {
			n := Sequence(start + i)
			if incVals[i] != n {
				t.Errorf("Inc(start=%d) returned %d when we were expecting %d", start, incVals[i], n)
			}
			if getVals[i] != n {
				t.Errorf("GetSequence(start=%d) returned %d when we were expecting %d", start, getVals[i], n)
			}
		}
	}
}

func loadTestData(t *testing.T, db DatabaseAPI) []string {
	var ids []string
	for i := 1; i <= 100; i++ {
		testIP := fmt.Sprintf("10.%d.%d.%d/32", rand.Intn(255), rand.Intn(255), rand.Intn(255)) // nolint: gosec
		ip, _ := datastore.ParseCIDR(testIP)
		peer := &datastore.Peer{
			PublicKey: fmt.Sprintf("peer%03d", i),
			Expire:    time.Now().AddDate(1, 0, 0),
			IP:        ip,
		}
		ids = append(ids, peer.PublicKey)
		if err := db.Add(peer); err != nil {
			t.Fatalf("Insert() error: %v", err)
		}
	}
	return ids
}

func dbInSync(t *testing.T, ids, absentIDs []string, dbs ...Database) {
	// Check keys that are expected to be present.
	for _, id := range ids {
		for i, db := range dbs {
			if _, ok := db.FindByPublicKey(id); !ok {
				t.Errorf("item '%s' not present in db #%d", id, i+1)
			}
		}
	}

	// Check a key that is expected to be absent.
	absentIDs = append([]string{"-- NON EXISTENT ID --"}, absentIDs...)
	for _, id := range absentIDs {
		for i, db := range dbs {
			if _, ok := db.FindByPublicKey(id); ok {
				t.Errorf("FindByPublicKey() on db #%d returned true on non-existent ID '%s'", i+1, id)
			}
		}
	}

	// Compare sequences.
	s0 := dbs[0].LatestSequence()
	for i := 1; i < len(dbs); i++ {
		si := dbs[i].LatestSequence()
		if si != s0 {
			t.Errorf("db #%d does not have the same sequence number as primary: %s, %s", i+1, si, s0)
		}
	}
}

func withSync(ctx context.Context, t *testing.T, m1 Log, m2 LogReceiver, f func(context.Context)) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := Follow(ctx, m1, m2); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Follow: %v", err)
		}
	}()

	f(ctx)

	<-ctx.Done()
	wg.Wait()
}

func runSimplePropagationTest(t *testing.T, m1, m2 Database) {
	defer m1.Close()
	defer m2.Close()

	// Load data on the first db.
	ids := loadTestData(t, m1)

	// Run a first sync process, verify propagation.
	withSync(context.Background(), t, m1, m2, func(_ context.Context) {})
	dbInSync(t, ids, nil, m1, m2)

	present, absent := ids[:len(ids)-1], ids[len(ids)-1:]
	upID := ids[92]

	// Run a second incremental sync process where we add an entry
	// at some point and delete another one.
	upIP, _ := datastore.ParseCIDR("1.2.3.4/16")
	withSync(context.Background(), t, m1, m2, func(_ context.Context) {
		time.Sleep(200 * time.Millisecond)
		if err := m1.Add(&datastore.Peer{
			PublicKey: "test",
		}); err != nil {
			t.Fatalf("Add() error: %v", err)
		}
		if err := m1.Update(&datastore.Peer{
			PublicKey: upID,
			IP:        upIP,
		}); err != nil {
			t.Fatalf("Update() error: %v", err)
		}
		if err := m1.Delete(&datastore.Peer{
			PublicKey: absent[0],
		}); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
	})
	// Force creation of a new array.
	present = append([]string{"test"}, present...)
	dbInSync(t, present, absent, m1, m2)

	peer, _ := m2.FindByPublicKey(upID)
	if peer.IP.String() != upIP.String() {
		t.Fatalf("Update() failed, object unchanged: %+v (expected ip=%s)", peer, upIP.String())
	}
}

func runStressPropagationTest(t *testing.T, db1, db2, db3 Database) {
	// Load data on the first db.
	var mx sync.Mutex
	ids := loadTestData(t, db1)

	withSync(context.Background(), t, db1, db2, func(ctx context.Context) {
		withSync(ctx, t, db1, db3, func(ctx context.Context) {
			var wg sync.WaitGroup

			for j := 0; j < 10; j++ {
				wg.Add(1)
				go func(j int) {
					defer wg.Done()
					i := 0
					for ctx.Err() == nil {
						time.Sleep(1 * time.Millisecond)
						id := fmt.Sprintf("test%03d.%06d", j, i)
						i++
						// nolint: errcheck
						db1.Add(&datastore.Peer{
							PublicKey: id,
						})
						mx.Lock()
						ids = append(ids, id)
						mx.Unlock()
					}
				}(j)
			}

			wg.Wait()
		})
	})

	// Final round to ensure syncronization in case we killed the
	// clients before Subscribe() was done.
	withSync(context.Background(), t, db1, db2, func(ctx context.Context) {
		withSync(ctx, t, db1, db3, func(_ context.Context) {
		})
	})

	dbInSync(t, ids, nil, db1, db2, db3)
}

func TestPropagation_Mem(t *testing.T) {
	runSimplePropagationTest(
		t,
		newDatabase(inMemoryDatabase(1000)),
		newDatabase(inMemoryDatabase(1000)),
	)
}

func TestPropagation_Mem_Stress(t *testing.T) {
	runStressPropagationTest(
		t,
		newDatabase(inMemoryDatabase(1000)),
		newDatabase(inMemoryDatabase(1000)),
		newDatabase(inMemoryDatabase(1000)),
	)
}

func TestPropagation_Mem_SmallHorizon(t *testing.T) {
	runSimplePropagationTest(
		t,
		newDatabase(inMemoryDatabase(10)),
		newDatabase(inMemoryDatabase(10)),
	)
}

func openSQLDatabaseImpl(t *testing.T, path string) (*sqlDatabase, func()) {
	sqldb, err := sqlite.OpenDB(path, datastore.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	return newSQLDatabaseImpl(sqldb, 0), func() {
		sqldb.Close()
	}
}

func TestPropagation_SQLToMem(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, cleanup := openSQLDatabaseImpl(t, dir+"/db1.sql")
	defer cleanup()

	runSimplePropagationTest(t, newDatabase(sql1), newDatabase(inMemoryDatabase(1000)))
}

func TestPropagation_MemToSQL(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, cleanup := openSQLDatabaseImpl(t, dir+"/db1.sql")
	defer cleanup()

	runSimplePropagationTest(t, newDatabase(inMemoryDatabase(1000)), newDatabase(sql1))
}

func TestPropagation_MemToSQL_SmallHorizon(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, cleanup := openSQLDatabaseImpl(t, dir+"/db1.sql")
	defer cleanup()

	runSimplePropagationTest(t, newDatabase(inMemoryDatabase(10)), newDatabase(sql1))
}

func TestPropagation_SQL(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, cleanup1 := openSQLDatabaseImpl(t, dir+"/db1.sql")
	defer cleanup1()

	sql2, cleanup2 := openSQLDatabaseImpl(t, dir+"/db2.sql")
	defer cleanup2()

	runSimplePropagationTest(t, newDatabase(sql1), newDatabase(sql2))
}

// func dumpDB(path string) {
// 	cmd := exec.Command("sh", "-c", "echo .d | sqlite3 "+path)
// 	cmd.Stdout = os.Stdout
// 	cmd.Run() // nolint: errcheck
// }

func TestPropagation_Remote(t *testing.T) {
	m1 := newDatabase(inMemoryDatabase(1000))
	m2 := newDatabase(inMemoryDatabase(1000))

	h := NewLogHTTPHandler(m1, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer h.Close()

	c := newRemotePubsubClient(srv.URL, nil)

	// Load data on the first db.
	ids := loadTestData(t, m1)

	// Run a first sync process, verify propagation.
	withSync(context.Background(), t, c, m2, func(_ context.Context) {})
	dbInSync(t, ids, nil, m1, m2)
}
