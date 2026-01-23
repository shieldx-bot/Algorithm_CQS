package rl

import (
	"sync"
)

type QTable struct {
	mu sync.RWMutex
	q  map[int]map[string]float64
	n  map[int]map[string]int
}

func NewQTable() *QTable {
	return &QTable{
		q: make(map[int]map[string]float64),
		n: make(map[int]map[string]int),
	}
}

func (t *QTable) Get(state int, action string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := t.q[state]
	if m == nil {
		return 0
	}
	return m[action]
}

func (t *QTable) Count(state int, action string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := t.n[state]
	if m == nil {
		return 0
	}
	return m[action]
}

// Update applies a simple exponential update: Q <- Q + alpha*(reward - Q)
// This is a contextual bandit baseline (no next-state TD term).
func (t *QTable) Update(state int, action string, reward float64, alpha float64) float64 {
	if alpha <= 0 {
		alpha = 0.1
	}
	if alpha > 1 {
		alpha = 1
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.q[state] == nil {
		t.q[state] = make(map[string]float64)
	}
	if t.n[state] == nil {
		t.n[state] = make(map[string]int)
	}

	old := t.q[state][action]
	newV := old + alpha*(reward-old)
	t.q[state][action] = newV
	t.n[state][action]++
	return newV
}

func (t *QTable) Snapshot() map[int]map[string]float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make(map[int]map[string]float64, len(t.q))
	for s, m := range t.q {
		cpy := make(map[string]float64, len(m))
		for a, v := range m {
			cpy[a] = v
		}
		out[s] = cpy
	}
	return out
}
