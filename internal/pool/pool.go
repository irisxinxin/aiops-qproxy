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
	// Wait for at least one session to be ready
	timer := time.NewTimer(30 * time.Second) // 增加等待时间到30秒，给Q CLI足够初始化时间
	select {
	case s := <-p.slots:
		timer.Stop()
		p.slots <- s
		log.Printf("pool: at least one session ready")
		return p, nil
	case <-timer.C:
		log.Printf("pool: no session ready after 30s; continuing with lazy fill")
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
		log.Printf("pool: attempting to create session (attempt %d)", attempt)
		s, err := qflow.New(ctx, p.opts)
		if err == nil {
			log.Printf("pool: session created successfully")
			select {
			case p.slots <- s:
				log.Printf("pool: session added to pool")
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
