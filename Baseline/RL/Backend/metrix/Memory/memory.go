//go:build linux

package memory

import (
	"math"
	"time"
)

// StartMemoryLogger emits one JSON record every 2 seconds into metrix/Memory/memory_logs/memory_metrics.jsonl.
// Output is flat (one-level JSON); fields not available are emitted as null.
func StartMemoryLogger() {
	const windowEvery = 2 * time.Second

	logger, cleanup, err := newMemoryMetricsFileLogger()
	if err != nil {
		return
	}
	defer func() { _ = cleanup() }()

	var prevWall time.Time
	prevWall = time.Now()

	var prevVM vmstatSnapshot
	var havePrevVM bool
	var prevCgroup cgroupMemSnap
	var havePrevCgroup bool
	var prevRSS int64
	var havePrevRSS bool
	var prevPauseTotal uint64
	var havePrevPause bool

	ticker := time.NewTicker(windowEvery)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		wallSec := now.Sub(prevWall).Seconds()
		if wallSec <= 0 {
			prevWall = now
			continue
		}

		timeStr := now.Format(tsLayout)
		metrics := map[string]interface{}{
			"time":       timeStr,
			"ts":         timeStr,
			"window_sec": wallSec,
		}

		// Pre-seed ALL requested metric keys as null. We'll fill what we can.
		for _, k := range allMemoryMetricKeys() {
			metrics[k] = nil
		}

		// ---- System memory capacity/utilization ----
		if mi, err := readMeminfo(); err == nil {
			metrics["mem_total"] = mi.memTotal
			metrics["mem_free"] = mi.memFree
			metrics["mem_available"] = mi.memAvailable
			memUsed := mi.memTotal - mi.memFree
			if memUsed < 0 {
				memUsed = 0
			}
			metrics["mem_used"] = memUsed
			if mi.memTotal > 0 {
				metrics["mem_used_percent"] = math.Round((float64(memUsed)/float64(mi.memTotal))*10000) / 100
				metrics["mem_available_percent"] = math.Round((float64(mi.memAvailable)/float64(mi.memTotal))*10000) / 100
			}

			metrics["swap_total"] = mi.swapTotal
			swapUsed := mi.swapTotal - mi.swapFree
			if swapUsed < 0 {
				swapUsed = 0
			}
			metrics["swap_used"] = swapUsed
			metrics["swap_free"] = mi.swapFree

			metrics["page_cache_size"] = mi.cached
			metrics["buffer_cache_size"] = mi.buffers
			metrics["slab_cache_size"] = mi.slab
			metrics["slab_reclaimable"] = mi.sReclaimable
			metrics["slab_unreclaimable"] = mi.sUnreclaim
		}

		// ---- Process / LB allocation ----
		rssBytes, sharedBytes, statmErr := readProcSelfStatm()
		if statmErr == nil {
			metrics["rss"] = rssBytes
			metrics["shared_memory"] = sharedBytes
			priv := rssBytes - sharedBytes
			if priv < 0 {
				priv = 0
			}
			metrics["private_memory"] = priv
			metrics["lb_rss"] = rssBytes

			if havePrevRSS {
				metrics["lb_memory_growth_rate"] = math.Round((float64(rssBytes-prevRSS)/wallSec)*10) / 10
			}
			prevRSS = rssBytes
			havePrevRSS = true
		}

		if st, err := readProcSelfStatus(); err == nil {
			if v, ok := st["VmSize"]; ok {
				metrics["vmsize"] = v
			}
			// If we have VmRSS from status and not statm, backfill.
			if metrics["rss"] == nil {
				if v, ok := st["VmRSS"]; ok {
					metrics["rss"] = v
					metrics["lb_rss"] = v
				}
			}
			if metrics["stack_size"] == nil {
				if v, ok := st["VmStk"]; ok {
					metrics["stack_size"] = v
				}
			}
			// Heap approximation via VmData (data segment)
			if v, ok := st["VmData"]; ok {
				metrics["heap_size"] = v
			}
		}

		ms := readGoMemStats()
		metrics["heap_size"] = int64(ms.HeapSys)
		metrics["heap_used"] = int64(ms.HeapAlloc)
		metrics["stack_size"] = int64(ms.StackInuse)
		metrics["lb_heap_used"] = int64(ms.HeapAlloc)

		if havePrevPause {
			dPauseNs := int64(ms.PauseTotalNs - prevPauseTotal)
			if dPauseNs < 0 {
				dPauseNs = 0
			}
			metrics["lb_gc_pause_time"] = math.Round((float64(dPauseNs)/1_000_000.0)*1000) / 1000
		}
		prevPauseTotal = ms.PauseTotalNs
		havePrevPause = true

		// ---- PSI / pressure ----
		if psi, ok := readMemoryPSIAvg10(); ok {
			metrics["memory_pressure"] = math.Round(psi*1000) / 1000
		}

		// ---- Paging / swap / reclaim / NUMA ----
		if vm, err := readVMStat(); err == nil {
			if havePrevVM {
				// page faults (delta per window)
				dMaj := int64(vm.pgmajfault - prevVM.pgmajfault)
				if dMaj < 0 {
					dMaj = 0
				}
				dFault := int64(vm.pgfault - prevVM.pgfault)
				if dFault < 0 {
					dFault = 0
				}
				dMin := dFault - dMaj
				if dMin < 0 {
					dMin = 0
				}
				metrics["page_faults_minor"] = dMin
				metrics["page_faults_major"] = dMaj

				// pgpgin/pgpgout are in kB
				dInKB := int64(vm.pgpginKB - prevVM.pgpginKB)
				dOutKB := int64(vm.pgpgoutKB - prevVM.pgpgoutKB)
				if dInKB < 0 {
					dInKB = 0
				}
				if dOutKB < 0 {
					dOutKB = 0
				}
				metrics["page_in_rate"] = math.Round((float64(dInKB)*1024.0/wallSec)*10) / 10
				metrics["page_out_rate"] = math.Round((float64(dOutKB)*1024.0/wallSec)*10) / 10

				// swap in/out are in pages
				ps := float64(pageSizeBytes())
				dSwapIn := int64(vm.pswpinPages - prevVM.pswpinPages)
				dSwapOut := int64(vm.pswpoutPages - prevVM.pswpoutPages)
				if dSwapIn < 0 {
					dSwapIn = 0
				}
				if dSwapOut < 0 {
					dSwapOut = 0
				}
				metrics["swap_in_rate"] = math.Round((float64(dSwapIn)*ps/wallSec)*10) / 10
				metrics["swap_out_rate"] = math.Round((float64(dSwapOut)*ps/wallSec)*10) / 10

				// reclaim + compaction
				dSteal := int64(vm.pgstealPages - prevVM.pgstealPages)
				if dSteal < 0 {
					dSteal = 0
				}
				metrics["memory_reclaim_rate"] = math.Round((float64(dSteal)*ps/wallSec)*10) / 10

				dCompact := int64(vm.compactStall - prevVM.compactStall)
				if dCompact < 0 {
					dCompact = 0
				}
				metrics["memory_compaction_rate"] = math.Round((float64(dCompact)/wallSec)*1000) / 1000

				// NUMA
				dLocal := int64(vm.localNode - prevVM.localNode)
				dRemote := int64(vm.otherNode - prevVM.otherNode)
				if dLocal < 0 {
					dLocal = 0
				}
				if dRemote < 0 {
					dRemote = 0
				}
				metrics["numa_local_access"] = dLocal
				metrics["numa_remote_access"] = dRemote

				dHit := float64(vm.numaHit - prevVM.numaHit)
				dMiss := float64(vm.numaMiss - prevVM.numaMiss)
				if dHit < 0 {
					dHit = 0
				}
				if dMiss < 0 {
					dMiss = 0
				}
				if dHit+dMiss > 0 {
					metrics["numa_miss_rate"] = math.Round((dMiss/(dHit+dMiss))*100000) / 100000
				}
			}

			prevVM = vm
			havePrevVM = true
		}

		// ---- Fragmentation ----
		if frag, ok := readMemoryFragmentationScore(); ok {
			metrics["memory_fragmentation"] = math.Round(frag*10000) / 10000
		}

		// ---- Cgroup / container memory ----
		if cg, err := readCgroupMemory(); err == nil {
			if cg.memoryMax != nil {
				metrics["memory_limit"] = int64(*cg.memoryMax)
			}
			metrics["memory_usage"] = int64(cg.memoryCurrent)
			if cg.inactiveFile != nil {
				ws := int64(cg.memoryCurrent) - int64(*cg.inactiveFile)
				if ws < 0 {
					ws = 0
				}
				metrics["memory_working_set"] = ws
			}

			// memory throttling / OOM
			if havePrevCgroup {
				dHigh := int64(cg.eventsHigh - prevCgroup.eventsHigh)
				if dHigh < 0 {
					dHigh = 0
				}
				metrics["memory_throttled_events"] = dHigh
			} else {
				metrics["memory_throttled_events"] = int64(0)
			}
			metrics["oom_kill_count"] = int64(cg.eventsOOMKill)

			prevCgroup = cg
			havePrevCgroup = true
		}

		// ---- Efficiency ----
		if mu, ok := metrics["mem_used"].(int64); ok && mu > 0 {
			if r, ok := metrics["rss"].(int64); ok && r >= 0 {
				metrics["useful_memory_ratio"] = math.Round((float64(r)/float64(mu))*100000) / 100000
				metrics["memory_overhead_lb"] = math.Round((float64(r)/float64(mu))*100000) / 100000
			}
		}

		// ---- Reliability (best-effort) ----
		if ecc, ok := readECCErrorCount(); ok {
			metrics["ecc_error_count"] = int64(ecc)
		}

		// Remaining keys stay nil: time-series, backend aggregates, DRAM BW/latency, cache_hit_ratio, poisoning/corruption, per-request.
		writeJSONLine(logger, metrics)
		prevWall = now
	}
}

func allMemoryMetricKeys() []string {
	return []string{
		// I. MEMORY CAPACITY & UTILIZATION
		"mem_total",
		"mem_used",
		"mem_free",
		"mem_available",
		"mem_used_percent",
		"mem_available_percent",
		// II. MEMORY ALLOCATION
		"rss",
		"vmsize",
		"shared_memory",
		"private_memory",
		"heap_size",
		"heap_used",
		"stack_size",
		// III. CACHE & BUFFER
		"page_cache_size",
		"buffer_cache_size",
		"slab_cache_size",
		"slab_reclaimable",
		"slab_unreclaimable",
		// IV. PRESSURE & CONTENTION
		"memory_pressure",
		"memory_reclaim_rate",
		"memory_compaction_rate",
		"memory_fragmentation",
		// V. PAGING & SWAP
		"page_faults_minor",
		"page_faults_major",
		"page_in_rate",
		"page_out_rate",
		"swap_total",
		"swap_used",
		"swap_free",
		"swap_in_rate",
		"swap_out_rate",
		// VI. LATENCY & ACCESS
		"memory_access_latency",
		"dram_read_bw",
		"dram_write_bw",
		"numa_local_access",
		"numa_remote_access",
		"numa_miss_rate",
		// VII. THROTTLING & LIMIT
		"memory_limit",
		"memory_usage",
		"memory_working_set",
		"memory_throttled_events",
		"oom_kill_count",
		// VIII. DYNAMICS
		"memory_usage_time_series",
		"memory_spike_frequency",
		"memory_spike_magnitude",
		"memory_usage_variance",
		"memory_growth_rate",
		// IX. FAIRNESS
		"memory_usage_mean_across_backends",
		"memory_usage_variance_across_backends",
		"memory_skew",
		"memory_max_minus_min",
		// X. EFFICIENCY
		"memory_per_request",
		"memory_overhead_lb",
		"useful_memory_ratio",
		"cache_hit_ratio",
		// XI. ERROR & RELIABILITY
		"ecc_error_count",
		"page_poisoning_events",
		"memory_corruption_events",
		// XII. LB-specific + backend aggregates
		"lb_rss",
		"lb_heap_used",
		"lb_memory_growth_rate",
		"lb_gc_pause_time",
		"backend_memory_mean",
		"backend_memory_p95",
		"backend_memory_variance",
	}
}
