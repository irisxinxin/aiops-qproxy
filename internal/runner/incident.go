package runner

import (
	"context"
	"os"
	"strings"

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

	// Always cleanup the session before releasing (avoid context leakage)
	defer func() {
		if e := s.ContextClear(); e != nil {
			lease.MarkBroken()
		}
		if e := s.Clear(); e != nil {
			lease.MarkBroken()
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
	if err != nil { // cleanup defer will run
		if qflow.IsConnError(err) {
			lease.MarkBroken()
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
