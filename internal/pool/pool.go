package pool

import (
	"context"
	"log"
	"math"
	"math/rand"
	"time"

	"aiops-qproxy/internal/qflow"
)

type Pool struct {
	size  int
	slots chan *qflow.Session
	opts  qflow.Opts
}

func New(ctx context.Context, size int, o qflow.Opts) (*Pool, error) {
	p := &Pool{
		size:  size,
		slots: make(chan *qflow.Session, size),
		opts:  o,
	}
	// Fill asynchronously; don't fail whole pool if some dials fail.
	for i := 0; i < size; i++ {
		go p.fillOne(ctx)
	}
	// Optionally wait briefly for at least one session
	timer := time.NewTimer(2 * time.Second)
	select {
	case s := <-p.slots:
		timer.Stop()
		p.slots <- s
		return p, nil
	case <-timer.C:
		log.Printf("pool: no session ready yet; continuing with lazy fill")
		return p, nil
	}
}

type Lease struct {
	p  *Pool
	s  *qflow.Session
	t0 time.Time
	// mark bad sessions so we don't put them back
	broken bool
}

func (p *Pool) Acquire(ctx context.Context) (*Lease, error) {
	select {
	case s := <-p.slots:
		return &Lease{p: p, s: s, t0: time.Now()}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) MarkBroken()             { l.broken = true }
func (l *Lease) Release() {
	if l.broken {
		// Replace it in background
		go l.p.fillOne(context.Background())
		return
	}
	l.p.slots <- l.s
}

func (p *Pool) fillOne(ctx context.Context) {
	backoff := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		s, err := qflow.New(ctx, p.opts)
		if err == nil {
			select {
			case p.slots <- s:
				return
			case <-ctx.Done():
				return
			}
		}
		sleep := withJitter(backoff)
		if backoff < 8*time.Second {
			backoff = time.Duration(math.Min(float64(backoff*2), float64(8*time.Second)))
		}
		log.Printf("pool: dial failed (attempt %d): %v; retry in %v", attempt, err, sleep)
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return
		}
	}
}

func withJitter(d time.Duration) time.Duration {
	delta := d / 5
	return d - delta + time.Duration(rand.Int63n(int64(2*delta)))
}

// Stats returns (ready,size).
func (p *Pool) Stats() (int, int) {
	return len(p.slots), p.size
}
