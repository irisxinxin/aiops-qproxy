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
	failedAttempts int32 // 连续失败次数（原子操作），用于避免无限重试
	healthy        int32 // 健康状态标记
}

func New(ctx context.Context, size int, o qflow.Opts) (*Pool, error) {
	p := &Pool{
		size:  size,
		slots: make(chan *qflow.Session, size),
		opts:  o,
	}

	// 预创建：启动后尽快填满池，使用更合理的间隔和错误处理
	go func() {
		for i := 0; i < size; i++ {
			if i > 0 {
				// 使用更长的间隔，避免同时创建太多连接
				time.Sleep(time.Duration(i*500) * time.Millisecond)
			}
			
			// 使用带超时的 context
			fillCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			success := p.fillOne(fillCtx)
			cancel()
			
			if !success {
				log.Printf("pool: failed to create initial session %d/%d", i+1, size)
				// 如果是 exec 模式失败，可能是 Q CLI 问题，继续尝试其他连接
				if p.opts.ExecMode {
					continue
				}
				// WebSocket 模式失败可能是服务不可用，等待更长时间再试
				time.Sleep(5 * time.Second)
			}
		}
		
		// 标记池为健康状态
		atomic.StoreInt32(&p.healthy, 1)
		log.Printf("pool: initialization completed, %d sessions ready", len(p.slots))
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
	// 检查池是否健康
	if atomic.LoadInt32(&p.healthy) == 0 {
		log.Printf("pool: not yet healthy, attempting direct creation")
	}
	
	// 最多尝试3次，避免拿到已被对端回收的旧连接
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case s := <-p.slots:
			// 轻量健康校验：使用更合理的超时时间
			hcTO := 1 * time.Second
			if dl, ok := ctx.Deadline(); ok {
				if rem := time.Until(dl); rem > 0 && rem < hcTO {
					hcTO = rem / 2 // 使用剩余时间的一半
				}
			}
			hcCtx, cancel := context.WithTimeout(ctx, hcTO)
			healthy := s.Healthy(hcCtx)
			cancel()
			
			if healthy {
				return &Lease{p: p, s: s, t0: time.Now()}, nil
			}
			
			// 不健康：关闭并同步创建一个替代
			log.Printf("pool: unhealthy session detected, replacing")
			_ = s.Close()
			
			// 尝试创建新会话
			createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			ns, err := qflow.New(createCtx, p.opts)
			cancel()
			
			if err == nil {
				return &Lease{p: p, s: ns, t0: time.Now()}, nil
			}
			
			log.Printf("pool: failed to create replacement session: %v", err)
			// 创建失败则继续下一轮（或落入下面的拨号）
			
		case <-ctx.Done():
			return nil, ctx.Err()
			
		default:
			// 没有可用连接：同步拨号
			log.Printf("pool: no available sessions, creating new one")
			createCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			ns, err := qflow.New(createCtx, p.opts)
			cancel()
			
			if err != nil {
				log.Printf("pool: direct session creation failed: %v", err)
				return nil, err
			}
			return &Lease{p: p, s: ns, t0: time.Now()}, nil
		}
	}
	
	// 理论上不会到这里，但提供一个最后的回退
	log.Printf("pool: all acquire attempts failed, trying one more direct creation")
	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ns, err := qflow.New(createCtx, p.opts)
	if err != nil {
		return nil, err
	}
	return &Lease{p: p, s: ns, t0: time.Now()}, nil
}

func (l *Lease) Session() *qflow.Session { return l.s }
func (l *Lease) MarkBroken()             { l.broken = true }

func (l *Lease) Release() {
	if l.broken {
		// Replace it in background
		_ = l.s.Close()
		
		// 限制并发 fillOne goroutine 数量，避免泄漏
		current := atomic.LoadInt32(&l.p.fillingWorkers)
		if current < int32(l.p.size*2) { // 允许最多 size*2 个 goroutine 在填充
			atomic.AddInt32(&l.p.fillingWorkers, 1)
			go func() {
				defer atomic.AddInt32(&l.p.fillingWorkers, -1)
				fillCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				l.p.fillOne(fillCtx)
			}()
		} else {
			log.Printf("pool: skipping fillOne, already %d workers running", current)
		}
		return
	}
	
	// 非阻塞归还，防止因通道已满导致调用方卡死
	select {
	case l.p.slots <- l.s:
		// 成功归还
	default:
		// 通道已满，关闭这个会话
		_ = l.s.Close()
		log.Printf("pool: slots full, dropping session on release")
	}
}

func (p *Pool) fillOne(ctx context.Context) bool {
	// 检查连续失败次数，避免无限重试
	failed := atomic.LoadInt32(&p.failedAttempts)
	if failed > 20 { // 增加最大失败次数，但添加更长的退避
		log.Printf("pool: too many consecutive failures (%d), refusing to retry", failed)
		return false
	}

	backoff := 1 * time.Second // 使用更合理的初始退避时间
	maxAttempts := 3 // 减少重试次数，但增加单次超时
	
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log.Printf("pool: attempting to create session (attempt %d/%d, total_failures=%d)", attempt, maxAttempts, failed)
		
		// 为每次拨号设置合理的超时
		dialTO := 45 * time.Second
		if p.opts.ExecMode {
			dialTO = 30 * time.Second // exec 模式可以更快
		}
		
		attemptCtx, cancel := context.WithTimeout(ctx, dialTO)
		s, err := qflow.New(attemptCtx, p.opts)
		cancel()
		
		if err == nil {
			log.Printf("pool: session created successfully")
			// 重置失败计数
			atomic.StoreInt32(&p.failedAttempts, 0)
			
			select {
			case p.slots <- s:
				log.Printf("pool: session added to pool")
				return true
			case <-ctx.Done():
				_ = s.Close()
				return false
			}
		}

		// 增加失败计数
		atomic.AddInt32(&p.failedAttempts, 1)
		log.Printf("pool: dial failed (attempt %d/%d): %v", attempt, maxAttempts, err)

		if attempt == maxAttempts {
			log.Printf("pool: max attempts reached, giving up (total_failures=%d)", atomic.LoadInt32(&p.failedAttempts))
			return false
		}

		// 使用指数退避，但有上限
		sleep := withJitter(backoff)
		if backoff < 10*time.Second {
			backoff = time.Duration(math.Min(float64(backoff*2), float64(10*time.Second)))
		}
		
		log.Printf("pool: retry in %v", sleep)
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return false
		}
	}
	
	return false
}

func withJitter(d time.Duration) time.Duration {
	delta := d / 4 // 减少抖动范围
	return d - delta + time.Duration(rand.Int63n(int64(2*delta)))
}

// Stats returns (ready,size).
func (p *Pool) Stats() (int, int) {
	return len(p.slots), p.size
}

// IsHealthy returns whether the pool has been successfully initialized
func (p *Pool) IsHealthy() bool {
	return atomic.LoadInt32(&p.healthy) > 0
}
