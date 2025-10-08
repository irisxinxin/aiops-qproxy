package pool

import (
	"context"
	"time"

	"aiops-qproxy/internal/qflow"
)

type Pool struct {
	slots chan *qflow.Session
}

func New(ctx context.Context, size int, o qflow.Opts) (*Pool, error) {
	p := &Pool{slots: make(chan *qflow.Session, size)}
	for i := 0; i < size; i++ {
		s, err := qflow.New(ctx, o)
		if err != nil {
			return nil, err
		}
		p.slots <- s
	}
	return p, nil
}

type Lease struct {
	p  *Pool
	s  *qflow.Session
	t0 time.Time
}

func (p *Pool) Acquire(ctx context.Context) (*Lease, error) {
	s := <-p.slots
	return &Lease{p: p, s: s, t0: time.Now()}, nil
}
func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) Release()                { l.p.slots <- l.s }
