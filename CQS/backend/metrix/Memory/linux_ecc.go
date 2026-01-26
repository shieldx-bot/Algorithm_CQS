//go:build linux

package memory

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readECCErrorCount() (uint64, bool) {
	// Best-effort: sum ce_count + ue_count across EDAC memory controllers if present.
	base := "/sys/devices/system/edac/mc"
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, false
	}
	var sum uint64
	var any bool
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "mc") {
			continue
		}
		for _, fn := range []string{"ce_count", "ue_count"} {
			b, err := os.ReadFile(filepath.Join(base, e.Name(), fn))
			if err != nil {
				continue
			}
			n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
			if err != nil {
				continue
			}
			sum += n
			any = true
		}
	}
	return sum, any
}
