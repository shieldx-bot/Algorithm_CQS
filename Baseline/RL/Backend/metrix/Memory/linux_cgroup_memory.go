//go:build linux

package memory

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type cgroupMemSnap struct {
	version int
	path    string

	memoryCurrent uint64
	memoryMax     *uint64
	inactiveFile  *uint64

	eventsHigh    uint64
	eventsOOM     uint64
	eventsOOMKill uint64
}

func readCgroupMemory() (cgroupMemSnap, error) {
	cgPath, version, err := discoverCgroupPathForMemory()
	if err != nil {
		return cgroupMemSnap{}, err
	}

	var base string
	if version == 2 {
		base = filepath.Join("/sys/fs/cgroup", cgPath)
	} else {
		base = filepath.Join("/sys/fs/cgroup/memory", cgPath)
	}

	var snap cgroupMemSnap
	snap.version = version
	snap.path = base

	if version == 2 {
		curB, err := os.ReadFile(filepath.Join(base, "memory.current"))
		if err != nil {
			return cgroupMemSnap{}, err
		}
		cur, err := strconv.ParseUint(strings.TrimSpace(string(curB)), 10, 64)
		if err != nil {
			return cgroupMemSnap{}, err
		}
		snap.memoryCurrent = cur

		maxB, err := os.ReadFile(filepath.Join(base, "memory.max"))
		if err == nil {
			maxS := strings.TrimSpace(string(maxB))
			if maxS != "max" {
				max, err := strconv.ParseUint(maxS, 10, 64)
				if err == nil {
					snap.memoryMax = &max
				}
			}
		}

		stat, _ := readKeyValueFile(filepath.Join(base, "memory.stat"))
		if stat != nil {
			if v, ok := stat["inactive_file"]; ok {
				n, err := strconv.ParseUint(v, 10, 64)
				if err == nil {
					snap.inactiveFile = &n
				}
			}
		}

		events, _ := readKeyValueFile(filepath.Join(base, "memory.events"))
		if events != nil {
			if v, ok := events["high"]; ok {
				n, _ := strconv.ParseUint(v, 10, 64)
				snap.eventsHigh = n
			}
			if v, ok := events["oom"]; ok {
				n, _ := strconv.ParseUint(v, 10, 64)
				snap.eventsOOM = n
			}
			if v, ok := events["oom_kill"]; ok {
				n, _ := strconv.ParseUint(v, 10, 64)
				snap.eventsOOMKill = n
			}
		}

		return snap, nil
	}

	// cgroup v1 (best-effort)
	curB, err := os.ReadFile(filepath.Join(base, "memory.usage_in_bytes"))
	if err != nil {
		return cgroupMemSnap{}, err
	}
	cur, err := strconv.ParseUint(strings.TrimSpace(string(curB)), 10, 64)
	if err != nil {
		return cgroupMemSnap{}, err
	}
	snap.memoryCurrent = cur

	limB, err := os.ReadFile(filepath.Join(base, "memory.limit_in_bytes"))
	if err == nil {
		lim, err := strconv.ParseUint(strings.TrimSpace(string(limB)), 10, 64)
		if err == nil {
			snap.memoryMax = &lim
		}
	}

	// working set approximation is hard in v1 without memory.stat details; leave nil.
	// events are also different; leave zeros.
	return snap, nil
}

func discoverCgroupPathForMemory() (string, int, error) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(string(b), "\n")

	var v2Path string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// v2: 0::/some/path
		if strings.HasPrefix(line, "0::") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) == 3 {
				v2Path = parts[2]
			}
		}
	}
	if v2Path != "" {
		return strings.TrimPrefix(v2Path, "/"), 2, nil
	}

	// v1: find a line that includes memory controller
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		controllers := strings.Split(parts[1], ",")
		for _, c := range controllers {
			if c == "memory" {
				return strings.TrimPrefix(parts[2], "/"), 1, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", 0, err
	}
	return "", 0, errors.New("no cgroup memory controller found")
}
