//go:build linux

package memory

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type vmstatSnapshot struct {
	pgfault      uint64
	pgmajfault   uint64
	pgpginKB     uint64
	pgpgoutKB    uint64
	pswpinPages  uint64
	pswpoutPages uint64
	pgstealPages uint64
	compactStall uint64
	numaHit      uint64
	numaMiss     uint64
	localNode    uint64
	otherNode    uint64
}

func readVMStat() (vmstatSnapshot, error) {
	f, err := os.Open("/proc/vmstat")
	if err != nil {
		return vmstatSnapshot{}, err
	}
	defer func() { _ = f.Close() }()

	var snap vmstatSnapshot
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		k := parts[0]
		v, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "pgfault":
			snap.pgfault = v
		case "pgmajfault":
			snap.pgmajfault = v
		case "pgpgin":
			snap.pgpginKB = v
		case "pgpgout":
			snap.pgpgoutKB = v
		case "pswpin":
			snap.pswpinPages = v
		case "pswpout":
			snap.pswpoutPages = v
		case "pgsteal_kswapd":
			snap.pgstealPages += v
		case "pgsteal_direct":
			snap.pgstealPages += v
		case "compact_stall":
			snap.compactStall = v
		case "numa_hit":
			snap.numaHit = v
		case "numa_miss":
			snap.numaMiss = v
		case "local_node":
			snap.localNode = v
		case "other_node":
			snap.otherNode = v
		}
	}
	if err := s.Err(); err != nil {
		return vmstatSnapshot{}, err
	}
	return snap, nil
}
