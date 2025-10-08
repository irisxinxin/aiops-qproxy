package runner

import (
	"context"
	"os"

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

	// 2) lease a session
	lease, err := o.pool.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer lease.Release()
	s := lease.Session()

	// 3) /load previous conversation if exists
	if _, err := os.Stat(convPath); err == nil {
		_ = s.Load(convPath)
	}

	// 4) ask with current prompt
	out, err := s.AskOnce(in.Prompt)
	if err != nil {
		return "", err
	}

	// 5) compact + save (overwrite), then clear context/history
	_ = s.Compact()
	_ = s.Save(convPath, true)
	_ = s.ContextClear()
	_ = s.Clear()

	return out, nil
}
