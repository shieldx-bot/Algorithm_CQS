//go:build linux

package memory

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type meminfoSnapshot struct {
	memTotal     int64
	memFree      int64
	memAvailable int64
	buffers      int64
	cached       int64
	swapTotal    int64
	swapFree     int64
	slab         int64
	sReclaimable int64
	sUnreclaim   int64
}

func readMeminfo() (meminfoSnapshot, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return meminfoSnapshot{}, err
	}
	defer func() { _ = f.Close() }()

	var snap meminfoSnapshot
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		unit := ""
		if len(parts) >= 3 {
			unit = parts[2]
		}
		if unit == "kB" {
			val *= 1024
		}
		switch key {
		case "MemTotal":
			snap.memTotal = val
		case "MemFree":
			snap.memFree = val
		case "MemAvailable":
			snap.memAvailable = val
		case "Buffers":
			snap.buffers = val
		case "Cached":
			snap.cached = val
		case "SwapTotal":
			snap.swapTotal = val
		case "SwapFree":
			snap.swapFree = val
		case "Slab":
			snap.slab = val
		case "SReclaimable":
			snap.sReclaimable = val
		case "SUnreclaim":
			snap.sUnreclaim = val
		}
	}
	if err := s.Err(); err != nil {
		return meminfoSnapshot{}, err
	}
	return snap, nil
}
