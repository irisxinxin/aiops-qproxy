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
	// start async fillers; do not fail hard if some dials fail
	for i := 0; i < size; i++ {
		go p.fillOne(context.WithValue(ctx, "slot", i))
	}
	// Wait briefly to see if at least one session arrives
	timer := time.NewTimer(2 * time.Second)
	select {
	case s := <-p.slots:
		// put back and proceed
		timer.Stop()
		p.slots <- s
		return p, nil
	case <-timer.C:
		// no sessions yet, but keep background fillers running; still return pool
		log.Printf("pool: no session ready yet; continuing with lazy fill")
		return p, nil
	}
}

type Lease struct {
	p  *Pool
	s  *qflow.Session
	t0 time.Time
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
func (l *Lease) Release()                { l.p.slots <- l.s }

func (p *Pool) fillOne(ctx context.Context) {
	j := 0
	backoff := 500 * time.Millisecond
	for {
		s, err := qflow.New(ctx, p.opts)
		if err == nil {
			select {
			case p.slots <- s:
				return
			case <-ctx.Done():
				return
			}
		}
		j++
		sleep := jitter(backoff)
		if backoff < 8*time.Second {
			backoff = time.Duration(math.Min(float64(backoff*2), float64(8*time.Second)))
		}
		log.Printf("pool: dial failed (attempt %d): %v; retrying in %v", j, err, sleep)
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return
		}
	}
}

func jitter(d time.Duration) time.Duration {
	delta := d / 5
	return d - delta + time.Duration(rand.Int63n(int64(2*delta)))
}
