//go:build linux

package cpu

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"syscall"
	"time"

	metricsdb "github/shieldx-bot/CQS/metrix/db"
)

type jsonLineLogger interface {
	Print(v ...any)
}

func marshalOrderedJSON(record map[string]interface{}) ([]byte, error) {
	// JSON objects are unordered, but emitting a stable order (with ts first)
	// makes log files easier to read and stream-process.
	priority := []string{"time", "ts", "window_sec"}

	seen := make(map[string]struct{}, len(record))
	remaining := make([]string, 0, len(record))
	for k := range record {
		seen[k] = struct{}{}
		remaining = append(remaining, k)
	}
	// Remove priority keys from remaining.
	for _, k := range priority {
		if _, ok := seen[k]; ok {
			for i := 0; i < len(remaining); i++ {
				if remaining[i] == k {
					remaining = append(remaining[:i], remaining[i+1:]...)
					i--
				}
			}
		}
	}
	sort.Strings(remaining)

	buf := &bytes.Buffer{}
	buf.WriteByte('{')
	first := true
	writeKV := func(k string, v interface{}) error {
		kb, err := json.Marshal(k)
		if err != nil {
			return err
		}
		vb, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
		return nil
	}

	for _, k := range priority {
		if v, ok := record[k]; ok {
			if err := writeKV(k, v); err != nil {
				return nil, err
			}
		}
	}
	for _, k := range remaining {
		if err := writeKV(k, record[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeJSONLine(logger jsonLineLogger, record map[string]interface{}) {
	b, err := marshalOrderedJSON(record)
	if err != nil {
		return
	}
	logger.Print(string(b))
}

func StartCPUP95Logger() {
	// Linux-only, high-accuracy CPU metrics logger.
	// Emits one JSON log record every 2 seconds.
	const windowEvery = 2 * time.Second
	const tsLayout = "2006/01/02 15:04:05"
	if !metricsdb.Enabled() {
		return
	}

	var prevSys procStatSnapshot
	var prevCgroup cgroupCPUStat
	var havePrevSys bool
	var havePrevCgroup bool
	var prevVol, prevInvol uint64
	var havePrevSelfCtx bool

	var prevRU syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &prevRU); err != nil {
		now := time.Now()
		timeStr := now.Format(tsLayout)
		_ = metricsdb.InsertCPUMetrics(map[string]interface{}{
			"time":  timeStr,
			"ts":    timeStr,
			"type":  "cpu_metrics_error",
			"where": "getrusage_init",
			"error": err.Error(),
		})
		return
	}
	tickHz := clockTicksPerSecond()
	pc := newPerfCollector()
	defer pc.Close()
	prevUserUs := cpuUserMicros(&prevRU)
	prevSysUs := cpuSystemMicros(&prevRU)
	prevWall := time.Now()

	ticker := time.NewTicker(windowEvery)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		wallSec := now.Sub(prevWall).Seconds()
		if wallSec <= 0 {
			prevWall = now
			continue
		}

		sysSnap, sysErr := readProcStat()
		cgSnap, cgErr := readCgroupCPUStat()
		load1, load5, load15, loadOK := readProcLoadAvg()
		vol, invol, selfCtxOK := readProcSelfStatusCtxSwitches()

		var ru syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
			timeStr := now.Format(tsLayout)
			_ = metricsdb.InsertCPUMetrics(map[string]interface{}{
				"time":  timeStr,
				"ts":    timeStr,
				"type":  "cpu_metrics_error",
				"where": "getrusage_loop",
				"error": err.Error(),
			})
			prevWall = now
			continue
		}
		userUs := cpuUserMicros(&ru)
		sysUs := cpuSystemMicros(&ru)
		dUserUs := userUs - prevUserUs
		dSysUs := sysUs - prevSysUs
		prevUserUs = userUs
		prevSysUs = sysUs
		lbCPUUs := float64(dUserUs + dSysUs)
		lbUsagePct := (lbCPUUs / (wallSec * 1_000_000)) * 100.0
		if lbUsagePct < 0 {
			lbUsagePct = 0
		}
		lbUsefulRatio := 0.0
		if dUserUs+dSysUs > 0 {
			lbUsefulRatio = float64(dUserUs) / float64(dUserUs+dSysUs)
		}

		timeStr := now.Format(tsLayout)
		metrics := map[string]interface{}{
			"time":       timeStr,
			"ts":         timeStr,
			"window_sec": wallSec,
		}

		// Pre-seed keys that may be unavailable (emit as null instead of missing).
		metrics["instructions_per_cycle"] = nil
		metrics["cpu_cycles_total"] = nil
		metrics["cpu_cycles_per_second"] = nil
		metrics["stall_cycles_frontend"] = nil
		metrics["stall_cycles_backend"] = nil
		metrics["llc_miss_rate"] = nil
		metrics["branch_misprediction_rate"] = nil
		metrics["lb_branch_miss_rate"] = nil
		metrics["cpu_throttled_time"] = nil
		metrics["cpu_throttled_periods"] = nil
		metrics["cpu_cfs_quota"] = nil
		metrics["cpu_cfs_period"] = nil
		metrics["cpu_quota_utilization"] = nil

		// Keep the output one-level JSON only (no nested objects).
		// Any metrics requiring extra privileges/instrumentation are left as nil or omitted.

		// Perf / micro-architecture counters (best-effort).
		if pc != nil {
			cur, err := pc.Read()
			if err != nil {
				metrics["perf_enabled"] = false
				metrics["perf_error"] = err.Error()
			} else {
				delta := pc.Delta(cur)
				metrics["perf_enabled"] = pc.enabled
				cycles := float64(delta["cpu_cycles_total"])
				instr := float64(delta["instructions_total"])
				if cycles > 0 {
					metrics["instructions_per_cycle"] = math.Round((instr/cycles)*1000) / 1000
				}
				branches := float64(delta["branch_instructions_total"])
				branchMiss := float64(delta["branch_misses_total"])
				if branches > 0 {
					metrics["branch_misprediction_rate"] = math.Round((branchMiss/branches)*100000) / 100000
				}
				cacheRefs := float64(delta["cache_references_total"])
				cacheMiss := float64(delta["cache_misses_total"])
				if cacheRefs > 0 {
					metrics["llc_miss_rate"] = math.Round((cacheMiss/cacheRefs)*100000) / 100000
				}

				// Required top-level names (CPU PERFORMANCE / CACHE)
				metrics["cpu_cycles_total"] = cur["cpu_cycles_total"]
				metrics["cpu_cycles_per_second"] = math.Round((float64(delta["cpu_cycles_total"])/wallSec)*10) / 10
				metrics["stall_cycles_frontend"] = delta["stall_cycles_frontend_total"]
				metrics["stall_cycles_backend"] = delta["stall_cycles_backend_total"]
				if v, ok := metrics["branch_misprediction_rate"]; ok {
					metrics["lb_branch_miss_rate"] = v
				}
			}
		}

		// LB process metrics
		metrics["lb_cpu_time_user_usec"] = userUs
		metrics["lb_cpu_time_system_usec"] = sysUs
		metrics["lb_cpu_usage_pct"] = math.Round(lbUsagePct*10) / 10
		if rss, ok := processRSSBytes(); ok {
			metrics["lb_rss_bytes"] = rss
		}

		// Required top-level LB/EFFICIENCY names
		metrics["lb_cpu_usage"] = math.Round(lbUsagePct*10) / 10
		metrics["useful_cpu_ratio"] = math.Round(lbUsefulRatio*1000) / 1000

		// System CPU utilization/time breakdown
		if sysErr == nil {
			prevSnap := prevSys
			if havePrevSys {
				b, ok := computeBreakdownPct(prevSnap.total, sysSnap.total)
				if ok {
					metrics["cpu_usage_total"] = math.Round(b.usageTotal*10) / 10
					metrics["cpu_idle"] = math.Round(b.idle*10) / 10
					metrics["cpu_user"] = math.Round(b.user*10) / 10
					metrics["cpu_system"] = math.Round(b.system*10) / 10
					metrics["cpu_iowait"] = math.Round(b.iowait*10) / 10
					metrics["cpu_irq"] = math.Round(b.irq*10) / 10
					metrics["cpu_softirq"] = math.Round(b.softirq*10) / 10
					metrics["cpu_steal"] = math.Round(b.steal*10) / 10

					// CPU time deltas (ticks) from /proc/stat
					dUserTicks := sysSnap.total.user - prevSnap.total.user
					dSystemTicks := sysSnap.total.system - prevSnap.total.system
					dIdleTicks := sysSnap.total.idle - prevSnap.total.idle
					dIowaitTicks := sysSnap.total.iowait - prevSnap.total.iowait
					dIrqTicks := sysSnap.total.irq - prevSnap.total.irq
					dSoftirqTicks := sysSnap.total.softirq - prevSnap.total.softirq
					dStealTicks := sysSnap.total.steal - prevSnap.total.steal

					metrics["cpu_time_user"] = float64(dUserTicks) / tickHz
					metrics["cpu_time_system"] = float64(dSystemTicks) / tickHz
					metrics["cpu_time_idle"] = float64(dIdleTicks) / tickHz
					metrics["cpu_time_iowait"] = float64(dIowaitTicks) / tickHz
					metrics["cpu_time_irq"] = float64(dIrqTicks) / tickHz
					metrics["cpu_time_softirq"] = float64(dSoftirqTicks) / tickHz
					metrics["cpu_time_steal"] = float64(dStealTicks) / tickHz
					metrics["cpu_steal_time"] = float64(dStealTicks) / tickHz

					// per-core usage
					per := make([]float64, 0, minInt(len(prevSnap.perCore), len(sysSnap.perCore)))
					for i := 0; i < minInt(len(prevSnap.perCore), len(sysSnap.perCore)); i++ {
						cb, ok := computeBreakdownPct(prevSnap.perCore[i], sysSnap.perCore[i])
						if !ok {
							continue
						}
						per = append(per, cb.usageTotal)
					}
					if len(per) > 0 {
						perRounded := make([]float64, 0, len(per))
						for _, v := range per {
							perRounded = append(perRounded, math.Round(v*10)/10)
						}
						metrics["cpu_usage_per_core"] = perRounded
						metrics["cpu_utilization_stddev"] = math.Round(stddev(per)*100) / 100
					}
				}
			}

			// Scheduling / contention based on /proc/stat deltas
			if havePrevSys {
				dCtxt := float64(sysSnap.ctxt - prevSnap.ctxt)
				metrics["context_switch_total"] = sysSnap.ctxt
				metrics["context_switch_rate"] = math.Round((dCtxt/wallSec)*10) / 10
				metrics["runqueue_length"] = sysSnap.procsRunning
			}
			if loadOK {
				metrics["load_average_1m"] = load1
				metrics["load_average_5m"] = load5
				metrics["load_average_15m"] = load15
			}
			if selfCtxOK {
				metrics["voluntary_context_switch"] = vol
				metrics["involuntary_context_switch"] = invol
				if havePrevSelfCtx {
					metrics["voluntary_context_switch_rate"] = math.Round((float64(vol-prevVol)/wallSec)*10) / 10
					metrics["involuntary_context_switch_rate"] = math.Round((float64(invol-prevInvol)/wallSec)*10) / 10

					// LB-specific alias
					metrics["lb_context_switch_rate"] = math.Round((float64((vol-prevVol)+(invol-prevInvol))/wallSec)*10) / 10
				}
				prevVol, prevInvol = vol, invol
				havePrevSelfCtx = true
			}

			prevSys = sysSnap
			havePrevSys = true
		} else {
			metrics["cpu_error"] = sysErr.Error()
		}

		// Cgroup quota/throttling (container/cloud)
		if cgErr == nil {
			metrics["cgroup_version"] = cgSnap.version
			metrics["cgroup_path"] = cgSnap.path
			metrics["cpu_throttled_periods_total"] = cgSnap.nrThrottled
			if havePrevCgroup {
				dThrottledUs := float64(cgSnap.throttledUsec - prevCgroup.throttledUsec)
				dPeriods := float64(cgSnap.nrPeriods - prevCgroup.nrPeriods)
				dThrottledPeriods := float64(cgSnap.nrThrottled - prevCgroup.nrThrottled)

				metrics["cpu_throttled_time_usec_rate"] = math.Round((dThrottledUs/wallSec)*10) / 10
				metrics["cpu_throttled_periods_rate"] = math.Round((dThrottledPeriods/wallSec)*10) / 10
				if dPeriods > 0 {
					metrics["cpu_throttled_period_ratio"] = math.Round((dThrottledPeriods/dPeriods)*1000) / 1000
				}
				metrics["cpu_throttled_time_pct"] = math.Round((dThrottledUs/(wallSec*1_000_000))*1000) / 1000

				// Required top-level names (THROTTLING)
				metrics["cpu_throttled_time"] = math.Round((dThrottledUs/1_000_000)*1000) / 1000
				metrics["cpu_throttled_periods"] = uint64(dThrottledPeriods)
			}
			if cgSnap.quotaUsec != nil {
				metrics["cpu_cfs_quota_usec"] = *cgSnap.quotaUsec
				metrics["cpu_cfs_quota"] = *cgSnap.quotaUsec
			}
			if cgSnap.periodUsec != nil {
				metrics["cpu_cfs_period_usec"] = *cgSnap.periodUsec
				metrics["cpu_cfs_period"] = *cgSnap.periodUsec
			}
			if cgSnap.quotaUsec != nil && cgSnap.periodUsec != nil && *cgSnap.periodUsec > 0 {
				quotaCores := float64(*cgSnap.quotaUsec) / float64(*cgSnap.periodUsec)
				metrics["cpu_quota_cores"] = math.Round(quotaCores*1000) / 1000
				if quotaCores > 0 {
					metrics["cpu_quota_utilization"] = math.Round((lbUsagePct/(quotaCores*100.0))*1000) / 1000
				}
			}
			prevCgroup = cgSnap
			havePrevCgroup = true
		} else {
			metrics["cgroup_error"] = cgErr.Error()
		}

		// JSON-lines output (ordered keys; time/ts first)
		_ = metricsdb.InsertCPUMetrics(metrics)
		prevWall = now
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
