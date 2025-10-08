package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aiops-qproxy/internal/qflow"
)

type Pool struct {
	slots chan *qflow.Session
	opts  qflow.Opts
	mu    sync.RWMutex
	// 连接创建时间，用于检测过期连接
	sessionTimes map[*qflow.Session]time.Time
	// 最大连接存活时间
	maxLifetime time.Duration
	// 目标池大小
	targetSize int
}

func New(ctx context.Context, size int, o qflow.Opts) (*Pool, error) {
	p := &Pool{
		slots:        make(chan *qflow.Session, size),
		opts:         o,
		sessionTimes: make(map[*qflow.Session]time.Time),
		maxLifetime:  30 * time.Minute, // 连接最大存活30分钟
		targetSize:   size,
	}

	for i := 0; i < size; i++ {
		s, err := qflow.New(ctx, o)
		if err != nil {
			return nil, err
		}
		p.slots <- s
		p.mu.Lock()
		p.sessionTimes[s] = time.Now()
		p.mu.Unlock()
	}
	return p, nil
}

type Lease struct {
	p  *Pool
	s  *qflow.Session
	t0 time.Time
}

func (p *Pool) Acquire(ctx context.Context) (*Lease, error) {
	// 尝试从池中获取连接
	select {
	case s := <-p.slots:
		// 检查连接是否过期或失效
		if p.isSessionExpired(s) || !p.isSessionValid(s) {
			// 连接已过期或失效，重新创建
			p.mu.Lock()
			delete(p.sessionTimes, s)
			p.mu.Unlock()

			newSession, err := qflow.New(ctx, p.opts)
			if err != nil {
				// 如果重新创建失败，返回错误
				// 注意：这会导致池中连接数减少，但这是可接受的
				return nil, fmt.Errorf("failed to recreate session: %v", err)
			}
			s = newSession
			p.mu.Lock()
			p.sessionTimes[s] = time.Now()
			p.mu.Unlock()
		}

		return &Lease{p: p, s: s, t0: time.Now()}, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// 检查会话是否过期
func (p *Pool) isSessionExpired(s *qflow.Session) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if createTime, exists := p.sessionTimes[s]; exists {
		return time.Since(createTime) > p.maxLifetime
	}
	return true // 如果找不到创建时间，认为已过期
}

// 检查会话是否有效
func (p *Pool) isSessionValid(s *qflow.Session) bool {
	// 使用更轻量的健康检查
	_, err := s.AskOnce("echo test")
	return err == nil
}
func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) Release() {
	// 检查连接是否还有效，如果无效则不归还到池中
	if l.p.isSessionExpired(l.s) || !l.p.isSessionValid(l.s) {
		// 连接已失效，不归还到池中
		l.p.mu.Lock()
		delete(l.p.sessionTimes, l.s)
		l.p.mu.Unlock()

		// 异步创建一个新连接来维持池大小
		go l.p.maintainPoolSize()
		return
	}

	// 连接有效，归还到池中
	l.p.slots <- l.s
}

// 维护池大小，确保池中始终有足够的连接
func (p *Pool) maintainPoolSize() {
	// 检查当前池大小
	currentSize := len(p.slots)
	if currentSize >= p.targetSize {
		return // 池大小正常，不需要补充
	}

	// 需要补充连接
	needed := p.targetSize - currentSize
	for i := 0; i < needed; i++ {
		if s, err := qflow.New(context.Background(), p.opts); err == nil {
			p.slots <- s
			p.mu.Lock()
			p.sessionTimes[s] = time.Now()
			p.mu.Unlock()
		}
	}
}
