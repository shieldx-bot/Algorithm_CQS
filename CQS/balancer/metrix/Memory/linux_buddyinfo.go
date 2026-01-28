//go:build linux

package memory

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// memory_fragmentation: rough 0..1 score.
// 0 means free memory mostly in low orders (less fragmented); 1 means mostly in higher orders.
func readMemoryFragmentationScore() (float64, bool) {
	f, err := os.Open("/proc/buddyinfo")
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()

	var totalFreePages float64
	var lowOrderPages float64

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		// Expect at least: Node 0, zone DMA, 1 2 3 ... (orders)
		if len(parts) < 6 {
			continue
		}
		// Orders start at the last fields.
		orderCounts := parts[4:]
		for order, c := range orderCounts {
			n, err := strconv.ParseFloat(c, 64)
			if err != nil {
				continue
			}
			pages := n * float64(uint64(1)<<uint(order))
			totalFreePages += pages
			if order <= 2 {
				lowOrderPages += pages
			}
		}
	}
	if err := s.Err(); err != nil {
		return 0, false
	}
	if totalFreePages <= 0 {
		return 0, false
	}
	// High fragmentation -> lowOrderPages is small.
	score := 1.0 - (lowOrderPages / totalFreePages)
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, true
}
