package model

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/crud"
	"git.autistici.org/ai3/attic/wig/datastore/crudlog"
	"git.autistici.org/ai3/attic/wig/datastore/sqlite"
	"github.com/google/go-cmp/cmp"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var testIntfName = "test01"

func loadTestData(t *testing.T, db crud.Writer) []string {
	key, _ := wgtypes.GenerateKey()

	gwip, _ := ParseCIDR("10.0.0.1/8")
	intf := &Interface{
		Name:       testIntfName,
		PrivateKey: key.String(),
		PublicKey:  key.PublicKey().String(),
		IP:         gwip,
	}
	if err := db.Create(context.Background(), intf); err != nil {
		t.Fatalf("Create(interface): %v", err)
	}

	var ids []string
	for i := 1; i <= 100; i++ {
		testIP := fmt.Sprintf("10.%d.%d.%d/32", rand.Intn(255), rand.Intn(255), rand.Intn(255)) // nolint: gosec
		ip, _ := ParseCIDR(testIP)
		peer := &Peer{
			Interface: testIntfName,
			PublicKey: fmt.Sprintf("peer%03d", i),
			Expire:    time.Now().AddDate(1, 0, 0),
			IP:        ip,
		}
		ids = append(ids, peer.PublicKey)
		if err := db.Create(context.Background(), peer); err != nil {
			t.Fatalf("Create(peer) error: %v", err)
		}
	}
	return ids
}

func withSync(ctx context.Context, t *testing.T, m1 crudlog.LogSource, m2 crudlog.LogSink, f func(context.Context)) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := crudlog.Follow(ctx, m1, m2); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Follow: %v", err)
		}
	}()

	f(ctx)

	<-ctx.Done()
	wg.Wait()
}

func sqlDBsInSync(t *testing.T, paths ...string) {
	var dumps []string
	for _, path := range paths {
		out, _ := exec.Command("sqlite3", path, ".d").CombinedOutput()
		dumps = append(dumps, string(out))
	}
	for i := 1; i < len(dumps); i++ {
		if diffs := cmp.Diff(dumps[0], dumps[i]); diffs != "" {
			t.Fatalf("databases %s and %s differ: %s", paths[0], paths[i], diffs)
		}
	}
}

func dbInSync(t *testing.T, ids, absentIDs []string, dbs ...crudlog.Log) {
	// // Check keys that are expected to be present.
	// for _, id := range ids {
	// 	for i, db := range dbs {
	// 		if _, ok := db.FindByPublicKey(id); !ok {
	// 			t.Errorf("item '%s' not present in db #%d", id, i+1)
	// 		}
	// 	}
	// }

	// // Check a key that is expected to be absent.
	// absentIDs = append([]string{"-- NON EXISTENT ID --"}, absentIDs...)
	// for _, id := range absentIDs {
	// 	for i, db := range dbs {
	// 		if _, ok := db.FindByPublicKey(id); ok {
	// 			t.Errorf("FindByPublicKey() on db #%d returned true on non-existent ID '%s'", i+1, id)
	// 		}
	// 	}
	// }

	// Compare sequences.
	s0 := dbs[0].LatestSequence()
	for i := 1; i < len(dbs); i++ {
		si := dbs[i].LatestSequence()
		if si != s0 {
			t.Fatalf("db #%d does not have the same sequence number as primary: %s, %s", i+1, si, s0)
		}
	}
}

func runSimplePropagationTest(t *testing.T, m1, m2 crudlog.Log, src crudlog.LogSource) {
	if src == nil {
		src = m1
	}

	ids := loadTestData(t, m1)

	// Run a first sync process, verify propagation.
	ctx := context.Background()
	withSync(ctx, t, src, m2, func(_ context.Context) {})
	dbInSync(t, ids, nil, m1, m2)

	present, absent := ids[:len(ids)-1], ids[len(ids)-1:]
	upID := ids[92]

	// Run a second incremental sync process where we add an entry
	// at some point and delete another one.
	upIP, _ := ParseCIDR("1.2.3.4/16")
	withSync(ctx, t, src, m2, func(_ context.Context) {
		time.Sleep(200 * time.Millisecond)
		if err := m1.Create(ctx, &Peer{
			PublicKey: "test",
			Interface: "test01",
		}); err != nil {
			t.Fatalf("Add() error: %v", err)
		}
		if err := m1.Update(ctx, &Peer{
			PublicKey: upID,
			Interface: "test01",
			IP:        upIP,
		}); err != nil {
			t.Fatalf("Update() error: %v", err)
		}
		if err := m1.Delete(ctx, &Peer{
			PublicKey: absent[0],
		}); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
	})
	// Force creation of a new array.
	present = append([]string{"test"}, present...)
	dbInSync(t, present, absent, m1, m2)

	// uh... how do we get a Finder?
	//peer, _ := m2.FindByPublicKey(upID)
	//if peer.IP.String() != upIP.String() {
	//	t.Fatalf("Update() failed, object unchanged: %+v (expected ip=%s)", peer, upIP.String())
	//}
}

func TestModel_SQL(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, err := sqlite.OpenDB(dir+"/db1.sql", datastore.Migrations)
	if err != nil {
		t.Fatalf("sql1: %v", err)
	}
	defer sql1.Close()

	sql2, err := sqlite.OpenDB(dir+"/db2.sql", datastore.Migrations)
	if err != nil {
		t.Fatalf("sql2: %v", err)
	}
	//defer dumpDB(dir + "/db2.sql")
	defer sql2.Close()

	db1 := crudlog.Wrap(
		sql1,
		Model,
		Model.Encoding(),
	)
	db2 := crudlog.Wrap(
		sql2,
		Model,
		Model.Encoding(),
	)

	runSimplePropagationTest(t, db1, db2, nil)
	sqlDBsInSync(t, dir+"/db1.sql", dir+"/db2.sql")
}

func TestModel_SQL_Remote(t *testing.T) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sql1, err := sqlite.OpenDB(dir+"/db1.sql", datastore.Migrations)
	if err != nil {
		t.Fatalf("sql1: %v", err)
	}
	defer sql1.Close()

	sql2, err := sqlite.OpenDB(dir+"/db2.sql", datastore.Migrations)
	if err != nil {
		t.Fatalf("sql2: %v", err)
	}
	//defer dumpDB(dir + "/db2.sql")
	defer sql2.Close()

	db1 := crudlog.Wrap(
		sql1,
		Model,
		Model.Encoding(),
	)
	db2 := crudlog.Wrap(
		sql2,
		Model,
		Model.Encoding(),
	)

	h := crudlog.NewLogSourceHTTPHandler(db1, Model.Encoding(), nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer h.Close()

	src := crudlog.NewRemoteLogSource(srv.URL, Model.Encoding(), new(http.Client))

	runSimplePropagationTest(t, db1, db2, src)
	sqlDBsInSync(t, dir+"/db1.sql", dir+"/db2.sql")
}

// nolint: unused
func dumpDB(path string) {
	time.Sleep(100 * time.Millisecond)
	cmd := exec.Command("sh", "-c", "echo .d | sqlite3 "+path)
	cmd.Stdout = os.Stdout
	cmd.Run() // nolint: errcheck
}
