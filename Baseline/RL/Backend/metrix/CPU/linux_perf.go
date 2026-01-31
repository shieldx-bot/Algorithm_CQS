//go:build linux

package cpu

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

type perfCollector struct {
	fds      map[string]int
	prev     map[string]uint64
	enabled  bool
	initErr  string
	recErrs  map[string]string
	pid      int
	cpu      int
	openedAt int64
}

func newPerfCollector() *perfCollector {
	pc := &perfCollector{
		fds:     make(map[string]int),
		prev:    make(map[string]uint64),
		recErrs: make(map[string]string),
		pid:     0,  // current process
		cpu:     -1, // all CPUs
	}

	// Best-effort: open a small, high-value set of counters.
	// These may fail depending on perf_event_paranoid / permissions.
	counters := map[string]uint64{
		"cpu_cycles_total":            unix.PERF_COUNT_HW_CPU_CYCLES,
		"instructions_total":          unix.PERF_COUNT_HW_INSTRUCTIONS,
		"branch_instructions_total":   unix.PERF_COUNT_HW_BRANCH_INSTRUCTIONS,
		"branch_misses_total":         unix.PERF_COUNT_HW_BRANCH_MISSES,
		"cache_references_total":      unix.PERF_COUNT_HW_CACHE_REFERENCES,
		"cache_misses_total":          unix.PERF_COUNT_HW_CACHE_MISSES,
		"stall_cycles_frontend_total": unix.PERF_COUNT_HW_STALLED_CYCLES_FRONTEND,
		"stall_cycles_backend_total":  unix.PERF_COUNT_HW_STALLED_CYCLES_BACKEND,
	}

	var openedAny bool
	for name, cfg := range counters {
		fd, err := openPerfCounter(pc.pid, pc.cpu, cfg)
		if err != nil {
			pc.recErrs[name] = err.Error()
			continue
		}
		pc.fds[name] = fd
		openedAny = true
	}

	if !openedAny {
		pc.enabled = false
		pc.initErr = "perf counters unavailable (permission or unsupported)"
		return pc
	}
	pc.enabled = true
	return pc
}

func (pc *perfCollector) Close() {
	for _, fd := range pc.fds {
		_ = unix.Close(fd)
	}
}

func openPerfCounter(pid, cpu int, config uint64) (int, error) {
	attr := unix.PerfEventAttr{
		Type:   unix.PERF_TYPE_HARDWARE,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Config: config,
	}
	attr.Bits |= unix.PerfBitExcludeKernel
	attr.Bits |= unix.PerfBitExcludeHv

	fd, err := unix.PerfEventOpen(&attr, pid, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func (pc *perfCollector) Read() (map[string]uint64, error) {
	if !pc.enabled {
		return nil, errors.New(pc.initErr)
	}
	out := make(map[string]uint64, len(pc.fds))
	buf := make([]byte, 8)
	for name, fd := range pc.fds {
		n, err := unix.Read(fd, buf)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if n != 8 {
			return nil, fmt.Errorf("read %s: short read %d", name, n)
		}
		out[name] = *(*uint64)(unsafe.Pointer(&buf[0]))
	}
	return out, nil
}

func (pc *perfCollector) Delta(cur map[string]uint64) map[string]uint64 {
	d := make(map[string]uint64, len(cur))
	for k, v := range cur {
		prev := pc.prev[k]
		if v >= prev {
			d[k] = v - prev
		} else {
			d[k] = 0
		}
		pc.prev[k] = v
	}
	return d
}
