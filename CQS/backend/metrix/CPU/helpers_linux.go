//go:build linux

package cpu

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func cpuTimeMicros(ru *syscall.Rusage) int64 {
	utime := ru.Utime
	stime := ru.Stime
	return int64(utime.Sec)*1_000_000 + int64(utime.Usec) + int64(stime.Sec)*1_000_000 + int64(stime.Usec)
}

func cpuUserMicros(ru *syscall.Rusage) int64 {
	utime := ru.Utime
	return int64(utime.Sec)*1_000_000 + int64(utime.Usec)
}

func cpuSystemMicros(ru *syscall.Rusage) int64 {
	stime := ru.Stime
	return int64(stime.Sec)*1_000_000 + int64(stime.Usec)
}

func processRSSBytes() (uint64, bool) {
	// /proc/self/statm: size resident shared text lib data dt
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * uint64(os.Getpagesize()), true
}

func readProcLoadAvg() (one, five, fifteen float64, ok bool) {
	f, err := os.Open("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, false
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	if !s.Scan() {
		return 0, 0, 0, false
	}
	parts := strings.Fields(s.Text())
	if len(parts) < 3 {
		return 0, 0, 0, false
	}

	one, err1 := strconv.ParseFloat(parts[0], 64)
	five, err2 := strconv.ParseFloat(parts[1], 64)
	fifteen, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return one, five, fifteen, true
}

func readProcSelfStatusCtxSwitches() (voluntary, involuntary uint64, ok bool) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	var gotVol, gotInvol bool
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "voluntary_ctxt_switches:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "voluntary_ctxt_switches:"))
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				voluntary = n
				gotVol = true
			}
			continue
		}
		if strings.HasPrefix(line, "nonvoluntary_ctxt_switches:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "nonvoluntary_ctxt_switches:"))
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				involuntary = n
				gotInvol = true
			}
			continue
		}
	}

	if gotVol || gotInvol {
		return voluntary, involuntary, true
	}
	return 0, 0, false
}
