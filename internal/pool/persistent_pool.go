package pool

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"aiops-qproxy/internal/execchat"
)

// PersistentPool manages a pool of long-running Q CLI sessions
type PersistentPool struct {
	size     int
	clients  chan *execchat.PersistentClient
	qBin     string
	mu       sync.RWMutex
	closed   bool
	warmupWg sync.WaitGroup
}

type PersistentLease struct {
	pool   *PersistentPool
	client *execchat.PersistentClient
}

func NewPersistentPool(size int, qBin string) (*PersistentPool, error) {
	if size <= 0 {
		size = 3
	}
	
	p := &PersistentPool{
		size:    size,
		clients: make(chan *execchat.PersistentClient, size),
		qBin:    qBin,
	}
	
	// Pre-create and warm up clients
	p.warmupWg.Add(size)
	for i := 0; i < size; i++ {
		go func(idx int) {
			defer p.warmupWg.Done()
			
			client := execchat.NewPersistentClient(qBin)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			
			if err := client.Start(ctx); err != nil {
				log.Printf("persistent_pool: failed to start client %d: %v", idx, err)
				return
			}
			
			if err := client.WaitReady(ctx); err != nil {
				log.Printf("persistent_pool: client %d not ready: %v", idx, err)
				client.Close()
				return
			}
			
			select {
			case p.clients <- client:
				log.Printf("persistent_pool: client %d ready and added to pool", idx)
			default:
				log.Printf("persistent_pool: pool full, closing client %d", idx)
				client.Close()
			}
		}(i)
	}
	
	// Wait for warmup to complete in background
	go func() {
		p.warmupWg.Wait()
		ready, total := p.Stats()
		log.Printf("persistent_pool: warmup completed, %d/%d clients ready", ready, total)
	}()
	
	return p, nil
}

func (p *PersistentPool) Acquire(ctx context.Context) (*PersistentLease, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, fmt.Errorf("pool is closed")
	}
	p.mu.RUnlock()
	
	// Try to get a client from the pool
	select {
	case client := <-p.clients:
		// Verify client is still healthy
		if err := client.Ping(ctx); err != nil {
			log.Printf("persistent_pool: unhealthy client, creating new one: %v", err)
			client.Close()
			
			// Create a new client
			newClient := execchat.NewPersistentClient(p.qBin)
			if err := newClient.Start(ctx); err != nil {
				return nil, fmt.Errorf("failed to create new client: %w", err)
			}
			if err := newClient.WaitReady(ctx); err != nil {
				newClient.Close()
				return nil, fmt.Errorf("new client not ready: %w", err)
			}
			client = newClient
		}
		
		return &PersistentLease{pool: p, client: client}, nil
		
	case <-time.After(5 * time.Second):
		// No client available, create a temporary one
		log.Printf("persistent_pool: no client available, creating temporary client")
		client := execchat.NewPersistentClient(p.qBin)
		if err := client.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to create temporary client: %w", err)
		}
		if err := client.WaitReady(ctx); err != nil {
			client.Close()
			return nil, fmt.Errorf("temporary client not ready: %w", err)
		}
		
		return &PersistentLease{pool: p, client: client}, nil
		
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *PersistentLease) Client() *execchat.PersistentClient {
	return l.client
}

func (l *PersistentLease) Release() {
	if l.pool == nil || l.client == nil {
		return
	}
	
	l.pool.mu.RLock()
	closed := l.pool.closed
	l.pool.mu.RUnlock()
	
	if closed {
		l.client.Close()
		return
	}
	
	// Try to return to pool, but don't block
	select {
	case l.pool.clients <- l.client:
		// Successfully returned to pool
	default:
		// Pool is full, close the client
		l.client.Close()
	}
}

func (p *PersistentPool) Stats() (int, int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients), p.size
}

func (p *PersistentPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return nil
	}
	
	p.closed = true
	close(p.clients)
	
	// Close all clients in the pool
	for client := range p.clients {
		client.Close()
	}
	
	return nil
}

// WaitReady waits for at least one client to be ready
func (p *PersistentPool) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ready, _ := p.Stats()
			if ready > 0 {
				return nil
			}
		}
	}
}
