package runner

import (
	"context"
	"os"
	"strings"

	"aiops-qproxy/internal/pool"
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
	IncidentKey string `json:"incident_key"`
	Prompt      string `json:"prompt"`
}

func (o *Orchestrator) Process(ctx context.Context, in IncidentInput) (string, error) {
	// 1) map incident_key → sop_id
	sopID, err := o.sopmap.GetOrCreate(in.IncidentKey)
	if err != nil {
		return "", err
	}
	convPath := o.conv.PathFor(sopID)

	// 2) lease a session with retry on connection error
	var out string
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		lease, err := o.pool.Acquire(ctx)
		if err != nil {
			return "", err
		}
		s := lease.Session()

		// 3) /load previous conversation if exists
		if _, err := os.Stat(convPath); err == nil {
			_ = s.Load(convPath)
		}

		// 4) ask with current prompt
		out, err = s.AskOnce(in.Prompt)
		if err != nil {
			// 如果是连接错误，释放当前连接并重试
			lease.Release()
			if isConnectionError(err) && i < maxRetries-1 {
				continue
			}
			return "", err
		}

		// 5) compact + save (overwrite), then clear context/history
		_ = s.Compact()
		_ = s.Save(convPath, true)
		_ = s.ContextClear()
		_ = s.Clear()

		// 成功完成，释放连接
		lease.Release()
		break
	}

	return out, nil
}

// 判断是否为连接错误
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	connectionErrors := []string{
		"broken pipe",
		"connection reset",
		"connection refused",
		"network is unreachable",
		"i/o timeout",
		"use of closed network connection",
	}

	for _, connErr := range connectionErrors {
		if strings.Contains(errStr, connErr) {
			return true
		}
	}

	return false
}
