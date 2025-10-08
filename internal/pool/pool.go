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
		// 只检查连接是否过期，不进行健康检查
		// 健康检查可能会触发连接断开，在实际使用时处理连接错误
		if p.isSessionExpired(s) {
			// 连接已过期，重新创建
			p.mu.Lock()
			delete(p.sessionTimes, s)
			p.mu.Unlock()

			newSession, err := qflow.New(ctx, p.opts)
			if err != nil {
				// 如果重新创建失败，异步补充连接
				go p.maintainPoolSize()
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
	// 使用更轻量的健康检查 - 使用 Q CLI 支持的命令
	_, err := s.AskOnce("/context")
	if err != nil {
		// 如果是连接错误，立即返回false，让连接池重新创建连接
		return false
	}
	return true
}
func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) Release() {
	// 检查连接是否过期，如果过期则不归还到池中
	if l.p.isSessionExpired(l.s) {
		// 连接已过期，不归还到池中
		l.p.mu.Lock()
		delete(l.p.sessionTimes, l.s)
		l.p.mu.Unlock()

		// 异步创建一个新连接来维持池大小
		go l.p.maintainPoolSize()
		return
	}

	// 连接未过期，归还到池中
	// 注意：不在这里进行健康检查，避免阻塞
	l.p.slots <- l.s
}

// 维护池大小，确保池中始终有足够的连接
func (p *Pool) maintainPoolSize() {
	// 需要补充连接
	needed := 1 // 每次只补充一个连接，避免竞态条件

	for i := 0; i < needed; i++ {
		if s, err := qflow.New(context.Background(), p.opts); err == nil {
			// 尝试放入连接，如果池已满则丢弃
			select {
			case p.slots <- s:
				p.mu.Lock()
				p.sessionTimes[s] = time.Now()
				p.mu.Unlock()
			default:
				// 池已满，丢弃这个连接
			}
		}
	}
}
