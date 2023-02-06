package crudlog

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

func (p *pubsub) AddSubscriber(subCh chan Op) func() {
	p.mx.Lock()
	defer p.mx.Unlock()

	el := p.subscribers.PushFront(subCh)
	return func() {
		p.removeSubscriber(el)
	}
}

func (p *pubsub) removeSubscriber(el *list.Element) {
	p.mx.Lock()
	p.subscribers.Remove(el)
	ch := el.Value.(chan Op)
	p.toClose = append(p.toClose, ch)
	p.mx.Unlock()
}

func (p *pubsub) Emit(op Op) {
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
