package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	cpu "github/shieldx-bot/CQS/metrix/CPU"
	memory "github/shieldx-bot/CQS/metrix/Memory"
)

var (
	A float64 = 0.8
	B float64 = 0.1
	C float64 = 0.1
	e float64 = 0.000001
)

var (
	latencyMu      sync.RWMutex
	latencyEwmaMs  = map[string]float64{} // backend IP -> EWMA latency (ms)
	latencyAlpha   = 0.2
	latencyScaleMs = 500.0 // normalize latency to [0..1] via /latencyScaleMs
	selectionTopK  = 4
	probeTimeout   = 2 * time.Second
)

var rdb = redis.NewClient(&redis.Options{
	Addr: "redis.postgre-db.svc.cluster.local:6379",
})

type VPStype struct {
	IP         string
	FreeQueue  int
	TotalQueue int
	Ra         float64
	Ramax      float64
	PCT        float64
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func LoadBalancerTuning() {
	if v, err := strconv.ParseFloat(getEnvDefault("LATENCY_EWMA_ALPHA", "0.2"), 64); err == nil && v > 0 && v <= 1 {
		latencyAlpha = v
	}
	if v, err := strconv.ParseFloat(getEnvDefault("LATENCY_SCALE_MS", "500"), 64); err == nil && v > 0 {
		latencyScaleMs = v
	}
	if v, err := strconv.Atoi(getEnvDefault("TOP_K", "4")); err == nil && v > 0 {
		selectionTopK = v
	}
	if v, err := strconv.ParseFloat(getEnvDefault("PROBE_TIMEOUT_SEC", "2"), 64); err == nil && v > 0 {
		probeTimeout = time.Duration(v * float64(time.Second))
	}
}

func getEnvDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func recordBackendLatency(ip string, latency time.Duration, success bool) {
	ms := float64(latency.Milliseconds())
	if ms <= 0 {
		ms = 1
	}
	if !success {
		// Penalize failures as very high latency.
		ms = math.Max(ms, latencyScaleMs*2)
	}

	latencyMu.Lock()
	defer latencyMu.Unlock()
	prev, ok := latencyEwmaMs[ip]
	if !ok || prev <= 0 || math.IsNaN(prev) || math.IsInf(prev, 0) {
		latencyEwmaMs[ip] = ms
		return
	}
	latencyEwmaMs[ip] = latencyAlpha*ms + (1-latencyAlpha)*prev
}

func latencyPenalty(ip string) float64 {
	latencyMu.RLock()
	ms, ok := latencyEwmaMs[ip]
	latencyMu.RUnlock()
	if !ok || ms <= 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0
	}
	// Normalize to [0..1] where 0 is good (low latency) and 1 is bad.
	return clamp(ms/latencyScaleMs, 0, 1)
}

var VPS = []VPStype{
	{IP: "backend.backend-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend2.backend2-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend3.backend3-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend4.backend4-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend5.backend5-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend6.backend6-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend7.backend7-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend8.backend8-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend9.backend9-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "backend10.backend10-ns.svc.cluster.local", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
}

func setRedis() {

	ctx := context.Background()

	for _, vps := range VPS {
		err := rdb.HSet(
			ctx,
			"vps:"+vps.IP,
			"FreeQueue", vps.FreeQueue,
			"TotalQueue", vps.TotalQueue,
			"Ra", vps.Ra,
			"Ramax", vps.Ramax,
			"PCT", vps.PCT,
		).Err()
		if err != nil {
			panic(err)
		}
	}
}

func SetupWeightValues() {
	A = 0.8
	B = 0.1
	C = 0.1
	err := rdb.HSet(
		context.Background(),
		"key:weightValue", "weightA", A, "weightB", B, "weightC", C,
	).Err()
	if err != nil {
		panic(err)
	}
}

func LoadWeightValues() {
	vals, err := rdb.HGetAll(context.Background(), "key:weightValue").Result()
	if err != nil {
		return
	}
	if v, err := strconv.ParseFloat(vals["weightA"], 64); err == nil {
		A = v
	}
	if v, err := strconv.ParseFloat(vals["weightB"], 64); err == nil {
		B = v
	}
	if v, err := strconv.ParseFloat(vals["weightC"], 64); err == nil {
		C = v
	}
}

func UpdateRedisPCT(vps []VPStype) {
	ctx := context.Background()

	if len(vps) == 0 {
		return
	}

	for _, vps := range vps {
		err := rdb.HSet(ctx, "vps:"+vps.IP, "PCT", vps.PCT).Err()
		if err != nil {
			panic(err)
		}
	}
}
func GetListVPS() []VPStype {
	var server []VPStype
	ctx := context.Background()
	keys, _ := rdb.Keys(ctx, "vps:*").Result()
	for _, k := range keys {
		b, err := rdb.HGetAll(ctx, k).Result()
		if err != nil {
			continue
		}

		var v VPStype
		v.IP = k[4:]
		v.FreeQueue, _ = strconv.Atoi(b["FreeQueue"])
		v.TotalQueue, _ = strconv.Atoi(b["TotalQueue"])
		v.Ra, _ = strconv.ParseFloat(b["Ra"], 64)
		v.Ramax, _ = strconv.ParseFloat(b["Ramax"], 64)
		v.PCT, _ = strconv.ParseFloat(b["PCT"], 64)
		server = append(server, v)
	}
	return server
}

func calculatePnew(CP float64) ([]VPStype, []VPStype, error) {

	var VPSS []VPStype
	server := GetListVPS()
	for _, server := range server {
		if server.TotalQueue == 0 {
			continue
		}
		if server.FreeQueue == 0 {
			continue
		}
		if server.Ramax == 0 {
			continue
		}
		if CP == 0 {
			CP = e
		}
		// CP is a global signal from the client; keep it bounded to avoid dominating.
		cpNorm := clamp(CP, 0, 1)

		var QP float64
		QP = 1.0 - float64(server.FreeQueue)/float64(server.TotalQueue)

		lp := latencyPenalty(server.IP)
		var PCT float64
		// Lower PCT is better.
		// Use latency as a penalty term (avoid rewarding a backend just because it handled more requests).
		PCT = (A * cpNorm) + (B * QP) + (C * lp)
		server.PCT = PCT
		VPSS = append(VPSS, server)
	}
	sort.Slice(VPSS, func(i, j int) bool {
		return VPSS[i].PCT < VPSS[j].PCT
	})
	var best []VPStype
	n := len(VPSS)
	if n == 0 {
		return nil, nil, fmt.Errorf("no backend available")
	}

	if n == 1 {
		best = append(best, VPSS[0])
	} else {
		k := min(selectionTopK, n)
		pool := VPSS[:k]
		i := rand.Intn(k)
		j := rand.Intn(k - 1)
		if j >= i {
			j++
		}
		v1 := pool[i]
		v2 := pool[j]
		// power-of-two: chọn backend ít áp lực hơn
		if v1.PCT <= v2.PCT {
			best = append(best, v1)
		} else {
			best = append(best, v2)
		}
		fmt.Println("Po2(top-k):", v1.IP, "vs", v2.IP, "->", best[0].IP)
	}
	return best, VPSS, nil

}
func DeleteAllRedis() error {
	ctx := context.Background()
	return rdb.Do(ctx, "FLUSHALL", "ASYNC").Err()
}
func HasAnyKey() (bool, error) {
	ctx := context.Background()

	keys, _, err := rdb.Scan(ctx, 0, "*", 1).Result()
	if err != nil {
		return false, err
	}

	return len(keys) > 0, nil
}

type CountServer struct {
	IP    string
	Count int
}

var ArrayServer []CountServer

var dispatchMu sync.Mutex
var dispatchCounts = map[string]int{}

const dispatchKey = "dispatch:counts"

func resetDispatchCounts() {
	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	dispatchCounts = map[string]int{}
	_ = rdb.Del(context.Background(), dispatchKey).Err()
}

func incrementDispatch(ip string) {
	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	dispatchCounts[ip]++
}

func pushDispatchCounts() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		dispatchMu.Lock()
		if len(dispatchCounts) == 0 {
			dispatchMu.Unlock()
			continue
		}
		payload := make(map[string]interface{}, len(dispatchCounts))
		for ip, count := range dispatchCounts {
			payload[ip] = count
		}
		dispatchMu.Unlock()
		_ = rdb.HSet(context.Background(), dispatchKey, payload).Err()
	}
}

func TestPhanBoServer(server []VPStype) {
	var check bool = false
	for i := 0; i < len(ArrayServer); i++ {
		if ArrayServer[i].IP == server[0].IP {
			check = true
			ArrayServer[i].Count += 1
			break
		}
	}
	if !check {
		ArrayServer = append(ArrayServer, CountServer{IP: server[0].IP, Count: 1})
	}
	fmt.Println("ArrayServer:", ArrayServer)
}

func main() {
	LoadBalancerTuning()

	hasKey, err := HasAnyKey()
	if err != nil {
		fmt.Println("Lỗi khi kiểm tra key trong redis:", err)
		panic(err)
	}
	if !hasKey {
		setRedis()
		SetupWeightValues()
		fmt.Println("Khởi tạo key trong redis")
	} else {
		LoadWeightValues()
		fmt.Println("Đã có key trong redis")
	}
	resetDispatchCounts()

	go cpu.StartCPUP95Logger()
	go memory.StartMemoryLogger()
	go pushDispatchCounts()

	router := gin.Default()
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong load balancer",
		})
	})

	type BalanceRequest struct {
		Query     string    `json:"query"`
		CP        float64   `json:"cp"`
		TimeStart time.Time `json:"time_start"`
	}

	router.POST("/balance", func(c *gin.Context) {
		var jsonData BalanceRequest

		if err := c.BindJSON(&jsonData); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		LoadWeightValues()
		server, allServers, err := calculatePnew(jsonData.CP)
		if err != nil {
			fmt.Println("Lỗi khi tính toán Pnew:", err)
			c.JSON(500, gin.H{"error": "Internal Server Error"})
			return
		}
		UpdateRedisPCT(allServers)
		incrementDispatch(server[0].IP)
		TestPhanBoServer(server)
		URL := "http://" + server[0].IP + ":5000/process"

		payload, err := json.Marshal(jsonData)
		if err != nil {
			fmt.Println("Lỗi khi encode JSON:", err)
			c.JSON(500, gin.H{"error": "Internal Server Error"})
			return
		}

		req, err := http.NewRequest(http.MethodPost, URL, bytes.NewReader(payload))
		if err != nil {
			fmt.Println("Lỗi khi tạo request:", err)
			c.JSON(500, gin.H{"error": "Internal Server Error"})
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: probeTimeout}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			recordBackendLatency(server[0].IP, time.Since(start), false)
			fmt.Println("Lỗi khi gọi backend:", err)
			c.JSON(502, gin.H{"error": "Bad Gateway"})
			return
		}
		defer resp.Body.Close()
		recordBackendLatency(server[0].IP, time.Since(start), resp.StatusCode < 500)

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println("Lỗi khi đọc response backend:", err)
			c.JSON(502, gin.H{"error": "Bad Gateway"})
			return
		}

		c.JSON(resp.StatusCode, gin.H{
			"status": "success",
			"data":   string(respBody),
		})
	})
	router.Run(":8085")
}
