package runner

import (
	"context"
	"log"
	"os"
	"strings"
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
	defer lease.Release()
	s := lease.Session()

	// Always cleanup the session before releasing (avoid context leakage)
	// 使用带超时的 context，避免清理操作长时间阻塞
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		// 串行执行清理，使用带超时的 context
		if e := s.ContextClearWithContext(cleanupCtx); e != nil {
			if qflow.IsConnError(e) {
				lease.MarkBroken()
			}
		}
		if e := s.ClearWithContext(cleanupCtx); e != nil {
			if qflow.IsConnError(e) {
				lease.MarkBroken()
			}
		}
	}()

	// 3) /load previous conversation if exists
	if _, err := os.Stat(convPath); err == nil {
		if e := s.Load(convPath); e != nil && qflow.IsConnError(e) {
			lease.MarkBroken()
			return "", e
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
	if e := s.Compact(); e != nil && qflow.IsConnError(e) {
		lease.MarkBroken()
		return "", e
	}
	if e := s.Save(convPath, true); e != nil && qflow.IsConnError(e) {
		lease.MarkBroken()
		return "", e
	}

	return out, nil
}
