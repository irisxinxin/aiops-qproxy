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

	// 先尝试创建一个连接，确保基本功能可用
	log.Printf("pool: attempting to create initial session...")
	s, err := qflow.New(ctx, o)
	if err != nil {
		log.Printf("pool: initial session creation failed: %v", err)
		// 不返回错误，继续异步填充
	} else {
		p.slots <- s
		log.Printf("pool: initial session created successfully")
	}

	// 异步创建其他连接，但限制并发数
	go func() {
		for i := 1; i < size; i++ {
			time.Sleep(time.Duration(i) * time.Second) // 间隔创建，避免过载
			p.fillOne(context.Background())
		}
	}()

	return p, nil
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
		_ = l.s.Close()
		go l.p.fillOne(context.Background())
		return
	}
	l.p.slots <- l.s
}

func (p *Pool) fillOne(ctx context.Context) {
	backoff := 500 * time.Millisecond
	maxAttempts := 10 // 最大重试次数
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log.Printf("pool: attempting to create session (attempt %d/%d)", attempt, maxAttempts)
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
		log.Printf("pool: dial failed (attempt %d/%d): %v; retry in %v", attempt, maxAttempts, err, sleep)

		if attempt == maxAttempts {
			log.Printf("pool: max attempts reached, giving up")
			return
		}

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
