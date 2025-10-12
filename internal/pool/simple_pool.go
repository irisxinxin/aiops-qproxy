package pool

import (
	"context"
	"fmt"
	"log"
	"sync"

	"aiops-qproxy/internal/execchat"
)

// SimplePool provides a lightweight connection pool for exec-based Q CLI clients
type SimplePool struct {
	size    int
	clients chan *execchat.SimpleExecClient
	qBin    string
	mu      sync.RWMutex
	closed  bool
}

type SimpleLease struct {
	pool   *SimplePool
	client *execchat.SimpleExecClient
}

func NewSimplePool(size int, qBin string) (*SimplePool, error) {
	if size <= 0 {
		size = 2
	}
	
	p := &SimplePool{
		size:    size,
		clients: make(chan *execchat.SimpleExecClient, size),
		qBin:    qBin,
	}
	
	// Pre-create clients (they're lightweight for exec mode)
	for i := 0; i < size; i++ {
		client := execchat.NewSimpleExecClient(qBin)
		select {
		case p.clients <- client:
		default:
			// Channel full, shouldn't happen
			log.Printf("simple_pool: channel full during init")
		}
	}
	
	log.Printf("simple_pool: initialized with %d clients", size)
	return p, nil
}

func (p *SimplePool) Acquire(ctx context.Context) (*SimpleLease, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, fmt.Errorf("pool is closed")
	}
	p.mu.RUnlock()
	
	select {
	case client := <-p.clients:
		return &SimpleLease{pool: p, client: client}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// No available client, create a new one
		client := execchat.NewSimpleExecClient(p.qBin)
		return &SimpleLease{pool: p, client: client}, nil
	}
}

func (l *SimpleLease) Client() *execchat.SimpleExecClient {
	return l.client
}

func (l *SimpleLease) Release() {
	if l.pool == nil || l.client == nil {
		return
	}
	
	l.pool.mu.RLock()
	closed := l.pool.closed
	l.pool.mu.RUnlock()
	
	if closed {
		return
	}
	
	// Try to return to pool, but don't block
	select {
	case l.pool.clients <- l.client:
		// Successfully returned to pool
	default:
		// Pool is full, just discard
	}
}

func (p *SimplePool) Stats() (int, int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients), p.size
}

func (p *SimplePool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return nil
	}
	
	p.closed = true
	close(p.clients)
	
	// Drain and close all clients
	for client := range p.clients {
		_ = client.Close()
	}
	
	return nil
}
