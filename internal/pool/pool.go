package pool

import (
	"context"
	"fmt"
	"time"

	"aiops-qproxy/internal/qflow"
)

type Pool struct {
	slots chan *qflow.Session
	opts  qflow.Opts
}

func New(ctx context.Context, size int, o qflow.Opts) (*Pool, error) {
	p := &Pool{
		slots: make(chan *qflow.Session, size),
		opts:  o,
	}
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

	// 检查会话是否还有效，如果无效则重新创建
	if s == nil || !p.isSessionValid(s) {
		// 重新创建会话
		newSession, err := qflow.New(ctx, p.opts)
		if err != nil {
			// 如果重新创建失败，返回错误
			return nil, fmt.Errorf("failed to recreate session: %v", err)
		}
		s = newSession
	}

	return &Lease{p: p, s: s, t0: time.Now()}, nil
}

// 检查会话是否有效
func (p *Pool) isSessionValid(s *qflow.Session) bool {
	// 简单的健康检查：尝试发送一个测试命令
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.AskOnce("echo test")
	return err == nil
}
func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) Release()                { l.p.slots <- l.s }
