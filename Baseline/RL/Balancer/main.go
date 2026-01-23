package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github/shieldx-bot/RL/rl"
)

type VPS struct {
	IP string `json:"ip"`

	LastQueue      int     `json:"last_queue"`
	LastTimeDoneMs int64   `json:"last_time_done_ms"`
	LastReward     float64 `json:"last_reward"`
	UpdatedAtMs    int64   `json:"updated_at_ms"`
}

type stateQueue struct {
	mu   sync.Mutex
	fifo []int
}

func (q *stateQueue) Push(state int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fifo = append(q.fifo, state)
}

func (q *stateQueue) Pop() (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.fifo) == 0 {
		return 0, false
	}
	v := q.fifo[0]
	q.fifo = q.fifo[1:]
	return v, true
}

var (
	listMu sync.RWMutex
	vps    []VPS

	dispatchStateByIP = map[string]*stateQueue{}
	qtable            = rl.NewQTable()
)

func getenvFloat(name string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func getenvInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func getenvString(name string, def string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	return v
}

func ensureDispatchQueue(ip string) *stateQueue {
	listMu.Lock()
	defer listMu.Unlock()
	q := dispatchStateByIP[ip]
	if q == nil {
		q = &stateQueue{}
		dispatchStateByIP[ip] = q
	}
	return q
}

func totalQueueSnapshot() int {
	listMu.RLock()
	defer listMu.RUnlock()
	total := 0
	for _, s := range vps {
		total += s.LastQueue
	}
	return total
}

func pickVPS(epsilon float64, maxQueue int) (string, int, error) {
	listMu.RLock()
	defer listMu.RUnlock()
	if len(vps) == 0 {
		return "", 0, fmt.Errorf("no vps configured")
	}

	totalQ := 0
	for _, s := range vps {
		totalQ += s.LastQueue
	}
	state := rl.StateIDFromTotalQueue(totalQ, maxQueue)

	if rand.Float64() < rl.Clamp01(epsilon) {
		idx := rand.Intn(len(vps))
		return vps[idx].IP, state, nil
	}

	bestIP := vps[0].IP
	bestQ := qtable.Get(state, bestIP)
	for i := 1; i < len(vps); i++ {
		ip := vps[i].IP
		q := qtable.Get(state, ip)
		if q > bestQ {
			bestQ = q
			bestIP = ip
		}
	}
	return bestIP, state, nil
}

func main() {
	rand.Seed(time.Now().UnixNano())

	defaultVPS := []VPS{
		{IP: "34.87.59.164"},
		{IP: "34.158.51.160"},
	}
	if env := strings.TrimSpace(os.Getenv("RL_VPS")); env != "" {
		parts := strings.Split(env, ",")
		var tmp []VPS
		for _, p := range parts {
			ip := strings.TrimSpace(p)
			if ip == "" {
				continue
			}
			tmp = append(tmp, VPS{IP: ip})
		}
		if len(tmp) > 0 {
			defaultVPS = tmp
		}
	}

	listMu.Lock()
	vps = defaultVPS
	for _, s := range vps {
		if dispatchStateByIP[s.IP] == nil {
			dispatchStateByIP[s.IP] = &stateQueue{}
		}
	}
	listMu.Unlock()

	epsilon := getenvFloat("RL_EPSILON", 0.10)
	alpha := getenvFloat("RL_ALPHA", 0.20)
	maxQueue := getenvInt("RL_MAX_QUEUE", 1000)
	port := getenvString("RL_PORT", "8084")

	router := gin.Default()

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong - RL load balancing baseline"})
	})

	router.GET("/debug/state", func(c *gin.Context) {
		listMu.RLock()
		vpsSnap := make([]VPS, len(vps))
		copy(vpsSnap, vps)
		listMu.RUnlock()

		c.JSON(200, gin.H{
			"epsilon":              epsilon,
			"alpha":                alpha,
			"max_queue":            maxQueue,
			"total_queue_snapshot": totalQueueSnapshot(),
			"vps":                  vpsSnap,
			"q":                    qtable.Snapshot(),
		})
	})

	router.POST("/load-test-http3", func(c *gin.Context) {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(400, gin.H{"error": "cannot read body"})
			return
		}

		ip, state, err := pickVPS(epsilon, maxQueue)
		if err != nil {
			c.JSON(503, gin.H{"error": err.Error()})
			return
		}
		ensureDispatchQueue(ip).Push(state)

		req, err := http.NewRequest(
			http.MethodPost,
			"http://"+ip+":8081/TestHTTP3",
			bytes.NewReader(bodyBytes),
		)
		if err != nil {
			c.JSON(500, gin.H{"error": "create request failed"})
			return
		}
		req.Header = c.Request.Header.Clone()
		req.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(502, gin.H{"error": "fetch failed"})
			return
		}
		defer resp.Body.Close()

		c.Header("X-RL-State", strconv.Itoa(state))
		c.Header("X-RL-Selected-IP", ip)
		c.Status(resp.StatusCode)
		_, _ = io.Copy(c.Writer, resp.Body)
	})

	router.POST("/receive-metrics", func(c *gin.Context) {
		type metricsIn struct {
			TimeDoneTask  int64   `json:"TimeDoneTask"`
			TimeStartSend int64   `json:"TimeStartSend"`
			Penumj        int64   `json:"Penumj"`
			Pemips        int64   `json:"Pemips"`
			NumberTask    int64   `json:"NumberTask"`
			TTj           float64 `json:"ttj"`
			TLi           int64   `json:"tli"`
			IPVM          string  `json:"ip_vm"`
			IFS           int     `json:"ifs"`
			VMbw          float64 `json:"vmbw"`
			TotalOnQueue  int64   `json:"total_on_queue"`
		}

		var in metricsIn
		timeNow := time.Now().UnixMilli()
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "invalid json"})
			return
		}
		ip := strings.TrimSpace(in.IPVM)
		if ip == "" {
			c.JSON(400, gin.H{"error": "missing ip_vm"})
			return
		}

		timeDone := float64(in.TimeDoneTask)
		if timeDone <= 0 {
			timeDone = 1
		}
		reward := 1.0 / (timeDone + 1e-9)

		state, ok := ensureDispatchQueue(ip).Pop()
		if !ok {
			state = rl.StateIDFromTotalQueue(totalQueueSnapshot(), maxQueue)
		}
		newQ := qtable.Update(state, ip, reward, alpha)

		listMu.Lock()
		found := false
		for i := range vps {
			if vps[i].IP == ip {
				vps[i].LastQueue = int(in.TotalOnQueue)
				vps[i].LastTimeDoneMs = in.TimeDoneTask
				vps[i].LastReward = reward
				vps[i].UpdatedAtMs = timeNow
				found = true
				break
			}
		}
		if !found {
			vps = append(vps, VPS{IP: ip, LastQueue: int(in.TotalOnQueue), LastTimeDoneMs: in.TimeDoneTask, LastReward: reward, UpdatedAtMs: timeNow})
			if dispatchStateByIP[ip] == nil {
				dispatchStateByIP[ip] = &stateQueue{}
			}
		}
		listMu.Unlock()

		out := gin.H{
			"ip":        ip,
			"state":     state,
			"reward":    reward,
			"q_updated": newQ,
		}
		if b, err := json.Marshal(in); err == nil {
			out["raw"] = string(b)
		}
		c.JSON(200, out)
	})

	_ = router.Run(":" + port)
}
