//go:build !linux

package cpu

import "syscall"

func cpuTimeMicros(_ *syscall.Rusage) int64 { return 0 }

func cpuUserMicros(_ *syscall.Rusage) int64 { return 0 }

func cpuSystemMicros(_ *syscall.Rusage) int64 { return 0 }

func processRSSBytes() (uint64, bool) { return 0, false }

func readProcLoadAvg() (one, five, fifteen float64, ok bool) { return 0, 0, 0, false }

func readProcSelfStatusCtxSwitches() (voluntary, involuntary uint64, ok bool) { return 0, 0, false }
