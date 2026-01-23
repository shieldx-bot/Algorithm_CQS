package rl

import "math"

// StateID is a small discrete representation of the current global load.
// We keep it tiny on purpose: stable and easy to compare against non-RL baselines.
//
// Buckets:
// - 0: low load
// - 1: medium load
// - 2: high load
// - 3: overloaded
//
// maxQueue is the queue value that roughly corresponds to "overloaded".
func StateIDFromTotalQueue(totalQueue int, maxQueue int) int {
	if maxQueue <= 0 {
		maxQueue = 1000
	}
	if totalQueue < 0 {
		totalQueue = 0
	}
	ratio := float64(totalQueue) / float64(maxQueue)

	switch {
	case ratio < 0.10:
		return 0
	case ratio < 0.35:
		return 1
	case ratio < 0.70:
		return 2
	default:
		return 3
	}
}

func Clamp01(x float64) float64 {
	return math.Max(0, math.Min(1, x))
}
