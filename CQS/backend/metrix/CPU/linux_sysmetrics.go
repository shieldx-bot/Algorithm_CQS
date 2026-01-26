//go:build linux

package cpu

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type procCPUTimes struct {
	user      uint64
	nice      uint64
	system    uint64
	idle      uint64
	iowait    uint64
	irq       uint64
	softirq   uint64
	steal     uint64
	guest     uint64
	guestNice uint64
}

func (t procCPUTimes) total() uint64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal + t.guest + t.guestNice
}

type procStatSnapshot struct {
	total        procCPUTimes
	perCore      []procCPUTimes
	ctxt         uint64
	procsRunning uint64
}

func readProcStat() (procStatSnapshot, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return procStatSnapshot{}, err
	}
	defer f.Close()

	var snap procStatSnapshot
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch {
		case fields[0] == "cpu":
			t, ok := parseCPUTimes(fields)
			if ok {
				snap.total = t
			}
		case strings.HasPrefix(fields[0], "cpu") && len(fields[0]) > 3 && fields[0][3] >= '0' && fields[0][3] <= '9':
			t, ok := parseCPUTimes(fields)
			if ok {
				snap.perCore = append(snap.perCore, t)
			}
		case fields[0] == "ctxt" && len(fields) >= 2:
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				snap.ctxt = v
			}
		case fields[0] == "procs_running" && len(fields) >= 2:
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				snap.procsRunning = v
			}
		}
	}
	if err := s.Err(); err != nil {
		return procStatSnapshot{}, err
	}
	return snap, nil
}

func parseCPUTimes(fields []string) (procCPUTimes, bool) {
	// Format: cpu  user nice system idle iowait irq softirq steal guest guest_nice
	// Some kernels omit trailing fields.
	if len(fields) < 5 {
		return procCPUTimes{}, false
	}
	vals := make([]uint64, 0, 10)
	for i := 1; i < len(fields) && len(vals) < 10; i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return procCPUTimes{}, false
		}
		vals = append(vals, v)
	}
	get := func(i int) uint64 {
		if i < len(vals) {
			return vals[i]
		}
		return 0
	}
	return procCPUTimes{
		user:      get(0),
		nice:      get(1),
		system:    get(2),
		idle:      get(3),
		iowait:    get(4),
		irq:       get(5),
		softirq:   get(6),
		steal:     get(7),
		guest:     get(8),
		guestNice: get(9),
	}, true
}

type cpuBreakdownPct struct {
	usageTotal float64
	idle       float64
	user       float64
	system     float64
	iowait     float64
	irq        float64
	softirq    float64
	steal      float64
}

func computeBreakdownPct(prev, cur procCPUTimes) (cpuBreakdownPct, bool) {
	prevTotal := prev.total()
	curTotal := cur.total()
	if curTotal <= prevTotal {
		return cpuBreakdownPct{}, false
	}
	dTotal := float64(curTotal - prevTotal)
	d := func(aPrev, aCur uint64) float64 {
		if aCur <= aPrev {
			return 0
		}
		return float64(aCur-aPrev) / dTotal * 100.0
	}

	userPct := d(prev.user+prev.nice, cur.user+cur.nice)
	sysPct := d(prev.system, cur.system)
	idlePct := d(prev.idle, cur.idle)
	iowaitPct := d(prev.iowait, cur.iowait)
	irqPct := d(prev.irq, cur.irq)
	softirqPct := d(prev.softirq, cur.softirq)
	stealPct := d(prev.steal, cur.steal)
	usageTotal := math.Max(0, 100.0-idlePct)

	return cpuBreakdownPct{
		usageTotal: usageTotal,
		idle:       idlePct,
		user:       userPct,
		system:     sysPct,
		iowait:     iowaitPct,
		irq:        irqPct,
		softirq:    softirqPct,
		steal:      stealPct,
	}, true
}

func stddev(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	mean := sum / float64(len(vals))
	var ss float64
	for _, v := range vals {
		d := v - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(vals)))
}

type cgroupCPUStat struct {
	usageUsec     uint64
	userUsec      uint64
	systemUsec    uint64
	nrPeriods     uint64
	nrThrottled   uint64
	throttledUsec uint64
	quotaUsec     *uint64
	periodUsec    *uint64
	version       int // 1 or 2
	path          string
}

func readCgroupCPUStat() (cgroupCPUStat, error) {
	// Best-effort: try cgroup v2 unified first using /proc/self/cgroup
	pathV2, okV2 := cgroupV2Path()
	if okV2 {
		st, err := readCgroupV2CPU(pathV2)
		if err == nil {
			return st, nil
		}
	}

	// Fall back to common v1 layout
	st, err := readCgroupV1CPU("/sys/fs/cgroup/cpu")
	if err == nil {
		return st, nil
	}
	// Some distros mount v1 at /sys/fs/cgroup/cpu,cpuacct
	st, err2 := readCgroupV1CPU("/sys/fs/cgroup/cpu,cpuacct")
	if err2 == nil {
		return st, nil
	}

	if okV2 {
		return cgroupCPUStat{}, fmt.Errorf("cgroup cpu stat not available (v2 path=%s)", pathV2)
	}
	return cgroupCPUStat{}, errors.New("cgroup cpu stat not available")
}

func cgroupV2Path() (string, bool) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// v2 format: 0::<path>
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" {
			return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(parts[2], "/")), true
		}
	}
	return "", false
}

func readKVFile(path string) (map[string]uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := make(map[string]uint64)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		v, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		m[parts[0]] = v
	}
	return m, nil
}

func readCgroupV2CPU(dir string) (cgroupCPUStat, error) {
	kv, err := readKVFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return cgroupCPUStat{}, err
	}
	st := cgroupCPUStat{
		usageUsec:     kv["usage_usec"],
		userUsec:      kv["user_usec"],
		systemUsec:    kv["system_usec"],
		nrPeriods:     kv["nr_periods"],
		nrThrottled:   kv["nr_throttled"],
		throttledUsec: kv["throttled_usec"],
		version:       2,
		path:          dir,
	}
	if b, err := os.ReadFile(filepath.Join(dir, "cpu.max")); err == nil {
		parts := strings.Fields(strings.TrimSpace(string(b)))
		if len(parts) == 2 {
			if parts[0] != "max" {
				if q, err := strconv.ParseUint(parts[0], 10, 64); err == nil {
					st.quotaUsec = &q
				}
			}
			if p, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				st.periodUsec = &p
			}
		}
	}
	return st, nil
}

func readCgroupV1CPU(dir string) (cgroupCPUStat, error) {
	kv, err := readKVFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return cgroupCPUStat{}, err
	}
	st := cgroupCPUStat{
		nrPeriods:   kv["nr_periods"],
		nrThrottled: kv["nr_throttled"],
		version:     1,
		path:        dir,
	}
	// v1 throttled_time is in ns
	if v, ok := kv["throttled_time"]; ok {
		st.throttledUsec = v / 1000
	}
	if b, err := os.ReadFile(filepath.Join(dir, "cpu.cfs_quota_us")); err == nil {
		v := strings.TrimSpace(string(b))
		if v != "-1" {
			if q, err := strconv.ParseUint(v, 10, 64); err == nil {
				st.quotaUsec = &q
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "cpu.cfs_period_us")); err == nil {
		v := strings.TrimSpace(string(b))
		if p, err := strconv.ParseUint(v, 10, 64); err == nil {
			st.periodUsec = &p
		}
	}
	return st, nil
}
