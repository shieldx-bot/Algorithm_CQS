//go:build linux

package cpu

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var (
	clkTckOnce sync.Once
	clkTckHz   float64 = 100
)

func clockTicksPerSecond() float64 {
	clkTckOnce.Do(func() {
		out, err := exec.Command("getconf", "CLK_TCK").Output()
		if err != nil {
			return
		}
		s := strings.TrimSpace(string(out))
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v <= 0 {
			return
		}
		clkTckHz = v
	})
	return clkTckHz
}
