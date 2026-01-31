//go:build !linux

package cpu

import "log"

func StartCPUP95Logger() {
	log.Printf("[cpu_metrics] disabled: linux-only")
}
