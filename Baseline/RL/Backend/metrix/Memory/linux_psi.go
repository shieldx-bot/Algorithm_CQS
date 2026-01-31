//go:build linux

package memory

import (
	"os"
	"strconv"
	"strings"
)

// Reads /proc/pressure/memory and returns avg10 percentage for "some" stalls.
func readMemoryPSIAvg10() (float64, bool) {
	b, err := os.ReadFile("/proc/pressure/memory")
	if err != nil {
		return 0, false
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Prefer "some" line.
		if strings.HasPrefix(line, "some ") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if strings.HasPrefix(f, "avg10=") {
					v := strings.TrimPrefix(f, "avg10=")
					n, err := strconv.ParseFloat(v, 64)
					if err != nil {
						return 0, false
					}
					return n, true
				}
			}
		}
	}
	return 0, false
}
