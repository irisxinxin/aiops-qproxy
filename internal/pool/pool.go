package pool

import (
	"context"
	"log"
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"aiops-qproxy/internal/qflow"
)

type Pool struct {
	size           int
	slots          chan *qflow.Session
	opts           qflow.Opts
	fillingWorkers int32 // 正在后台填充的 goroutine 数量（原子操作）
}

func New(ctx context.Context, size int, o qflow.Opts) (*Pool, error) {
	p := &Pool{
		size:  size,
		slots: make(chan *qflow.Session, size),
		opts:  o,
	}

	// 异步创建所有连接，第一个立即尝试，后续间隔创建避免过载
	go func() {
		for i := 0; i < size; i++ {
			if i > 0 {
				time.Sleep(time.Duration(i) * time.Second)
			}
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
	for {
		select {
		case s := <-p.slots:
			// 检查 session 是否健康
			if !s.Healthy(ctx) {
				log.Printf("pool: acquired session is unhealthy, replacing it")
				_ = s.Close()
				// 异步填充新的 session
				go p.fillOne(context.Background())
				continue // 继续等待下一个 session
			}
			return &Lease{p: p, s: s, t0: time.Now()}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) MarkBroken()             { l.broken = true }
func (l *Lease) Release() {
	if l.broken {
		// Replace it in background
		_ = l.s.Close()
		// 限制并发 fillOne goroutine 数量，避免泄漏
		current := atomic.LoadInt32(&l.p.fillingWorkers)
		if current < int32(l.p.size) { // 最多同时有 size 个 goroutine 在填充
			atomic.AddInt32(&l.p.fillingWorkers, 1)
			go func() {
				defer atomic.AddInt32(&l.p.fillingWorkers, -1)
				l.p.fillOne(context.Background())
			}()
		} else {
			log.Printf("pool: skipping fillOne, already %d workers running", current)
		}
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
