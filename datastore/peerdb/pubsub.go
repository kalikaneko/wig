package peerdb

import (
	"container/list"
	"sync"
)

type pubsub struct {
	mx          sync.Mutex
	toClose     []chan Op
	subscribers *list.List
}

func newPubSub() *pubsub {
	return &pubsub{
		subscribers: list.New(),
	}
}

func (p *pubsub) addSubscriber(subCh chan Op) *list.Element {
	p.mx.Lock()
	defer p.mx.Unlock()
	return p.subscribers.PushFront(subCh)
}

func (p *pubsub) removeSubscriber(el *list.Element) {
	p.mx.Lock()
	p.subscribers.Remove(el)
	ch := el.Value.(chan Op)
	p.toClose = append(p.toClose, ch)
	p.mx.Unlock()
}

func (p *pubsub) publish(op Op) {
	p.mx.Lock()
	if len(p.toClose) > 0 {
		for _, ch := range p.toClose {
			close(ch)
		}
		p.toClose = p.toClose[:0]
	}

	for e := p.subscribers.Front(); e != nil; e = e.Next() {
		ch := e.Value.(chan Op)
		ch <- op
	}
	p.mx.Unlock()
}

type subscription struct {
	preload []Op
	ch      chan Op

	p  *pubsub
	el *list.Element
}

func (s *subscription) Notify() <-chan Op {
	outCh := make(chan Op, chanBufSize)
	go func() {
		for _, op := range s.preload {
			outCh <- op
		}
		for op := range s.ch {
			outCh <- op
		}
	}()
	return outCh
}

func (s *subscription) Close() {
	s.p.removeSubscriber(s.el)
}
