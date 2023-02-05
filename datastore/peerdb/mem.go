package peerdb

import (
	"git.autistici.org/ai3/attic/wig/datastore"
)

type memLog struct {
	buf            []Op
	head, tail, sz int
	minSeq         Sequence
}

func newMemLog(n int) *memLog {
	return &memLog{
		buf: make([]Op, n+1),
		sz:  n,
	}
}

func (l *memLog) appendOp(op Op) {
	l.buf[l.head] = op
	l.head++
	if l.head > l.sz {
		l.head = 0
	}
	if l.head == l.tail {
		l.tail++
		if l.tail > l.sz {
			l.tail = 0
		}
	}
	l.minSeq = l.buf[l.tail].Seq
}

func (l *memLog) since(seq Sequence) ([]Op, error) {
	if seq < l.minSeq {
		return nil, ErrHorizon
	}

	var out []Op
	for i := l.tail; i != l.head; i++ {
		if i > l.sz {
			i = 0
		}
		if l.buf[i].Seq >= seq {
			out = append(out, l.buf[i])
		}
	}
	return out, nil
}

type memSnapshot struct {
	SeqNum Sequence          `json:"seq"`
	Items  []*datastore.Peer `json:"items"`
}

func (s *memSnapshot) Seq() Sequence {
	return s.SeqNum
}

func (s *memSnapshot) Each(f func(*datastore.Peer) error) error {
	for _, peer := range s.Items {
		if err := f(peer); err != nil {
			return err
		}
	}
	return nil
}

type memDB map[string]*datastore.Peer

func newMemDB() *memDB {
	m := make(memDB)
	return &m
}

func (m memDB) Insert(peer *datastore.Peer) error {
	m[peer.PublicKey] = peer
	return nil
}

func (m memDB) Update(peer *datastore.Peer) error {
	return m.Insert(peer)
}

func (m memDB) Delete(pk string) error {
	delete(m, pk)
	return nil
}

func (m *memDB) DropAll() {
	*m = make(memDB)
}

func (m memDB) Size() int { return len(m) }

func (m memDB) Each(f func(*datastore.Peer)) {
	for _, peer := range m {
		f(peer)
	}
}

func (m memDB) FindByPublicKey(pk string) (*datastore.Peer, bool) {
	peer, ok := m[pk]
	return peer, ok
}

type memSequencer struct {
	cur Sequence
}

func newMemSequencer(start Sequence) *memSequencer {
	return &memSequencer{cur: start}
}

func (s *memSequencer) Inc() Sequence {
	i := s.cur
	s.cur++
	return i
}

func (s *memSequencer) GetSequence() Sequence {
	return s.cur
}

func (s *memSequencer) SetSequence(n Sequence) error {
	if n < s.cur {
		return ErrOutOfSequence
	}
	s.cur = n
	return nil
}

type memDatabase struct {
	*memLog
	*memSequencer
	*memDB
}

func (d *memDatabase) Close() {}

func (d *memDatabase) newTransaction() (transaction, error) {
	return d, nil
}

func (d *memDatabase) commit() error { return nil }

func (d *memDatabase) rollback() {}

func inMemoryDatabase(n int) *memDatabase {
	return &memDatabase{
		memDB:        newMemDB(),
		memLog:       newMemLog(n),
		memSequencer: newMemSequencer(1),
	}
}

func NewInMemoryDatabase(logSz int) Database {
	if logSz == 0 {
		logSz = 10000
	}
	return newDatabase(inMemoryDatabase(logSz))
}
