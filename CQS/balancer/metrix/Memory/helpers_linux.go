//go:build linux

package memory

import (
	"bufio"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const tsLayout = "2006/01/02 15:04:05"

func pageSizeBytes() int64 {
	return int64(os.Getpagesize())
}

func readKeyValueFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	out := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseMeminfoBytes(kv map[string]string, key string) (int64, bool) {
	v, ok := kv[key]
	if !ok {
		return 0, false
	}
	// /proc/meminfo values are kB.
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n * 1024, true
}

func parseStatusBytes(status map[string]string, key string) (int64, bool) {
	v, ok := status[key]
	if !ok {
		return 0, false
	}
	// values are already in bytes in our parsing, but keep for future.
	_ = v
	return 0, false
}

func readProcSelfStatus() (map[string]int64, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	out := map[string]int64{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		// Example: VmRSS:\t  12345 kB
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		k := strings.TrimSpace(parts[0])
		rest := strings.TrimSpace(parts[1])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		unit := ""
		if len(fields) >= 2 {
			unit = fields[1]
		}
		switch unit {
		case "kB":
			n *= 1024
		case "mB", "MB":
			n *= 1024 * 1024
		}
		out[k] = n
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func readProcSelfStatm() (residentBytes, sharedBytes int64, err error) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, errors.New("statm: unexpected format")
	}
	resPages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	sharedPages, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	ps := pageSizeBytes()
	return resPages * ps, sharedPages * ps, nil
}

func readGoMemStats() *runtime.MemStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return &ms
}
