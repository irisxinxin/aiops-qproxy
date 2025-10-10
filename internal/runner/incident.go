package runner

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"aiops-qproxy/internal/pool"
	"aiops-qproxy/internal/qflow"
	"aiops-qproxy/internal/store"
)

type Orchestrator struct {
	pool   *pool.Pool
	sopmap *store.SOPMap
	conv   *store.ConvStore
}

func NewOrchestrator(p *pool.Pool, m *store.SOPMap, cs *store.ConvStore) *Orchestrator {
	return &Orchestrator{pool: p, sopmap: m, conv: cs}
}

type IncidentInput struct {
	IncidentKey string `json:"incident_key"` // 原始的 incident_key（用于 sopmap）
	SopID       string `json:"sop_id"`       // 可选：如果已知 sop_id，直接使用
	Prompt      string `json:"prompt"`
}

func (o *Orchestrator) Process(ctx context.Context, in IncidentInput) (string, error) {
	// 1) 确定 sop_id
	var sopID string
	var err error

	if in.SopID != "" {
		// 如果已提供 sop_id，直接使用，并更新映射
		sopID = in.SopID
		if in.IncidentKey != "" {
			// 记录 incident_key → sop_id 映射
			_ = o.sopmap.Set(in.IncidentKey, sopID)
		}
	} else {
		// 否则，通过 incident_key 生成或获取 sop_id
		sopID, err = o.sopmap.GetOrCreate(in.IncidentKey)
		if err != nil {
			return "", err
		}
	}

	convPath := o.conv.PathFor(sopID)
	log.Printf("runner: processing incident_key=%s → sop_id=%s, conv_path=%s",
		in.IncidentKey, sopID, convPath)

	// 2) lease a session
	lease, err := o.pool.Acquire(ctx)
	if err != nil {
		return "", err
	}
	s := lease.Session()

	// 注意：不在这里 defer Release()，而是在函数最后手动 Release
	// 原因：defer 会在函数返回前执行清理，但这时 AskOnce 可能还在等待响应
	var releaseOnce sync.Once
	doRelease := func() {
		releaseOnce.Do(func() {
			lease.Release()
		})
	}
	defer doRelease() // 确保无论如何都会释放

	// 3) /load previous conversation if exists
	if _, err := os.Stat(convPath); err == nil {
		log.Printf("runner: executing /load %s", convPath)
		if e := s.Load(convPath); e != nil {
			if qflow.IsConnError(e) {
				lease.MarkBroken()
				log.Printf("runner: /load failed (conn): %v", e)
				return "", e
			}
			log.Printf("runner: /load failed: %v", e)
		} else {
			log.Printf("runner: /load ok")
		}
	}

	// 4) ask with current prompt
	out, err := s.AskOnce(strings.TrimSpace(in.Prompt))
	if err != nil {
		if qflow.IsConnError(err) {
			lease.MarkBroken()
			// 连接错误时，关闭底层连接，避免 defer 中的清理操作继续使用已失效的连接
			_ = s.Close()
		}
		return "", err
	}

	// 5) compact + save (overwrite)
	log.Printf("runner: executing /compact")
	if e := s.Compact(); e != nil {
		if qflow.IsConnError(e) {
			lease.MarkBroken()
			log.Printf("runner: /compact failed (conn): %v", e)
			return "", e
		}
		log.Printf("runner: /compact failed: %v", e)
	} else {
		log.Printf("runner: /compact ok")
	}
	log.Printf("runner: executing /save %s (force)", convPath)
	if e := s.Save(convPath, true); e != nil {
		if qflow.IsConnError(e) {
			lease.MarkBroken()
			log.Printf("runner: /save failed (conn): %v", e)
			return "", e
		}
		log.Printf("runner: /save failed: %v", e)
	} else {
		log.Printf("runner: /save ok")
	}

	// 6) 清理 session context（成功完成后才清理）
	// 使用带超时的 context，避免清理操作阻塞
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()

	// 清理 Q CLI 的上下文，打印执行结果
	log.Printf("runner: executing /context clear (cleanup)")
	if e := s.ContextClearWithContext(cleanupCtx); e != nil {
		if qflow.IsConnError(e) {
			lease.MarkBroken()
			log.Printf("runner: /context clear failed (conn): %v", e)
		} else {
			log.Printf("runner: /context clear failed: %v", e)
		}
	} else {
		log.Printf("runner: /context clear ok")
	}
	log.Printf("runner: executing /clear (cleanup)")
	if e := s.ClearWithContext(cleanupCtx); e != nil {
		if qflow.IsConnError(e) {
			lease.MarkBroken()
			log.Printf("runner: /clear failed (conn): %v", e)
		} else {
			log.Printf("runner: /clear failed: %v", e)
		}
	} else {
		log.Printf("runner: /clear ok")
	}

	return out, nil
}
