package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

var (
	currentRate           int64   = 2 // req/s mặc định
	CPnew                 float64 = 1
	CPold                 float64 = 1
	rdb                   *redis.Client
	Apsilon               float64 = 0.02
	LatencyOld            float64 = 1
	LatencyNew            float64 = 1
	emaAlpha              float64 = 0.2
	cpEma                 float64 = 1
	latencyEma            float64 = 1
	weightCooldown                = 45 * time.Second
	lastWeightUpdateMu    sync.Mutex
	lastWeightUpdateByKey = map[string]time.Time{}

	metricsWindowSec = 10
)

type latencyWindow struct {
	mu          sync.Mutex
	latenciesMs []int64
	totalSent   int64
	lost        int64
}

var window = latencyWindow{latenciesMs: make([]int64, 0, 4096)}

func recordLatencySample(ms int64) {
	if ms <= 0 {
		ms = 1
	}
	window.mu.Lock()
	// Cap samples to avoid unbounded memory at high RPS.
	if len(window.latenciesMs) < 20000 {
		window.latenciesMs = append(window.latenciesMs, ms)
	}
	window.mu.Unlock()
}

func recordRequestSent() {
	window.mu.Lock()
	window.totalSent++
	window.mu.Unlock()
}

func recordLostRequest() {
	window.mu.Lock()
	window.lost++
	window.mu.Unlock()
}

func percentile(sorted []int64, p float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	idx := int(float64(n)*p + 0.999999) // ceil
	idx -= 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func pushClientMetricsLoop() {
	clientID := os.Getenv("CLIENT_ID")
	if clientID == "" {
		clientID = os.Getenv("HOSTNAME")
	}
	if clientID == "" {
		clientID = "default"
	}
	key := "client:metrics:" + clientID

	ticker := time.NewTicker(time.Duration(metricsWindowSec) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		window.mu.Lock()
		lats := make([]int64, len(window.latenciesMs))
		copy(lats, window.latenciesMs)
		total := window.totalSent
		lost := window.lost
		window.latenciesMs = window.latenciesMs[:0]
		window.totalSent = 0
		window.lost = 0
		window.mu.Unlock()

		sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
		p90 := percentile(lats, 0.90)
		p95 := percentile(lats, 0.95)

		payload := map[string]interface{}{
			"window_sec": metricsWindowSec,
			"ts":         time.Now().Format(time.RFC3339Nano),
			"total":      total,
			"lost":       lost,
			"p90_ms":     p90,
			"p95_ms":     p95,
		}
		_ = rdb.HSet(context.Background(), key, payload).Err()
	}
}

func loadTuning() {
	if v := os.Getenv("WEIGHT_EPSILON"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 0.2 {
			Apsilon = f
		}
	}
	if v := os.Getenv("WEIGHT_COOLDOWN_SEC"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 5 {
			weightCooldown = time.Duration(s) * time.Second
		}
	}
}

func canUpdateWeight(key string) bool {
	lastWeightUpdateMu.Lock()
	defer lastWeightUpdateMu.Unlock()
	last, ok := lastWeightUpdateByKey[key]
	if ok && time.Since(last) < weightCooldown {
		return false
	}
	lastWeightUpdateByKey[key] = time.Now()
	return true
}

type Job struct {
	Action float64 `json:"action"`
	Type   string  `json:"type"`
}

var JobChannel = make(chan Job, 1000)

type Frequency struct {
	Count int64     // internal counter
	Time  time.Time // last updated time
	Pold  float64   // previous count
	Pnew  float64   // new count
	CP    float64   // current processing rate
}

var Freq = Frequency{Count: 1, Time: time.Now(), Pold: 1, Pnew: 1}

func startRequestWorker(ctx context.Context, backendURL string) {
	go func() {
		var ticker *time.Ticker
		var lastRate int64 = 0

		for {
			select {
			case <-ctx.Done():
				if ticker != nil {
					ticker.Stop()
				}
				return

			default:
				rate := atomic.LoadInt64(&currentRate)

				// Nếu rate thay đổi → recreate ticker
				if rate != lastRate {
					if ticker != nil {
						ticker.Stop()
					}
					interval := time.Second / time.Duration(rate)
					ticker = time.NewTicker(interval)
					lastRate = rate
					log.Println("Rate changed to:", rate, "req/s")
				}

				<-ticker.C
				sendRequest(backendURL)
			}
		}
	}()
}
func sendRequest(url string) {
	if time.Now().Sub(Freq.Time) >= 10*time.Second {
		var temp float64 = Freq.Pnew
		if Freq.Pold == 0 {
			Freq.Pold = 1
		}
		Freq.Pnew = float64(Freq.Pold)*0.7 + float64(Freq.Count)*0.3
		Freq.Pold = temp
		Freq.Count = 0
		Freq.Time = time.Now()
		Freq.CP = (Freq.Pnew - Freq.Pold) / (Freq.Pold + 0.0001)
	} else {
		Freq.Count++
	}

	var CP float64
	CP = Freq.CP
	temp := CPnew
	CPnew = CP
	CPold = temp
	var Tile float64
	Tile = CPnew / (CPold + 0.0001)
	cpEma = (emaAlpha * Tile) + ((1 - emaAlpha) * cpEma)
	JobChannel <- Job{Action: cpEma, Type: "CP"}
	type Payload struct {
		CP        float64   `json:"cp"`
		TimeStart time.Time `json:"time_start"`
		Query     string    `json:"query,omitempty"`
	}
	var payload Payload
	payload.CP = CP
	payload.TimeStart = time.Now()
	PayloadRequest, err := json.Marshal(payload)
	if err != nil {
		log.Println("json marshal error:", err)
		return
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(PayloadRequest))
	if err != nil {
		log.Println("new request error:", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		recordRequestSent()
		recordLostRequest()
		log.Println("request error:", err)
		return
	}
	recordRequestSent()
	fmt.Printf("Response status code: %d\n", resp.StatusCode)
	TimeNow := time.Now()
	TailLatency := TimeNow.Sub(payload.TimeStart).Milliseconds()
	LatencyNew = float64(TailLatency)
	recordLatencySample(TailLatency)
	if resp.StatusCode >= 400 {
		// Treat non-success as lost from the client's perspective.
		recordLostRequest()
	}
	latencyRatio := LatencyNew / (LatencyOld + 0.0001)
	LatencyOld = LatencyNew
	latencyEma = (emaAlpha * latencyRatio) + ((1 - emaAlpha) * latencyEma)
	JobChannel <- Job{Action: latencyEma, Type: "Latency"}

	defer resp.Body.Close()
}

func ensureWeightValues() {
	ctx := context.Background()
	// Initialize defaults if the hash or any field is missing.
	if ok, _ := rdb.HSetNX(ctx, "key:weightValue", "weightA", 0.8).Result(); ok {
		// field was newly set
	}
	if ok, _ := rdb.HSetNX(ctx, "key:weightValue", "weightB", 0.1).Result(); ok {
	}
	if ok, _ := rdb.HSetNX(ctx, "key:weightValue", "weightC", 0.1).Result(); ok {
	}
	normalizeWeights()
}

func updateWeight(field string, factor float64) {
	if !canUpdateWeight(field) {
		return
	}
	ctx := context.Background()
	val, err := rdb.HGet(ctx, "key:weightValue", field).Float64()
	if err == redis.Nil {
		ensureWeightValues()
		val, err = rdb.HGet(ctx, "key:weightValue", field).Float64()
	}
	if err != nil {
		log.Println("redis HGet error:", err)
		return
	}
	val = val * factor
	if val < 0.1 {
		val = 0.1
	} else if val > 1 {
		val = 1
	}
	if err := rdb.HSet(ctx, "key:weightValue", field, val).Err(); err != nil {
		log.Println("redis HSet error:", err)
		return
	}
	normalizeWeights()
}

func normalizeWeights() {
	ctx := context.Background()
	vals, err := rdb.HGetAll(ctx, "key:weightValue").Result()
	if err != nil {
		return
	}
	a, _ := strconv.ParseFloat(vals["weightA"], 64)
	b, _ := strconv.ParseFloat(vals["weightB"], 64)
	c, _ := strconv.ParseFloat(vals["weightC"], 64)
	a = clamp(a, 0.1, 1)
	b = clamp(b, 0.1, 1)
	c = clamp(c, 0.1, 1)
	sum := a + b + c
	if sum == 0 {
		return
	}
	a /= sum
	b /= sum
	c /= sum
	_ = rdb.HSet(ctx, "key:weightValue", "weightA", a, "weightB", b, "weightC", c).Err()
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

func StartWorker() {
	for {
		job := <-JobChannel
		_ = job
		if job.Type == "CP" {
			if job.Action > 1.70 {
				updateWeight("weightA", 1+Apsilon)
				continue
			} else if job.Action > 1.40 {
				updateWeight("weightA", 1+Apsilon/2)
				continue
			}
		}
		if job.Type == "Latency" {
			if job.Action > 1.70 {
				updateWeight("weightB", 1+Apsilon)
				continue
			}
			if job.Action > 1.40 {
				updateWeight("weightB", 1+Apsilon/2)
				continue
			}
			if job.Action <= 0.30 {
				updateWeight("weightB", 1-Apsilon)
				continue
			}
			if job.Action <= 0.60 {
				updateWeight("weightB", 1-Apsilon/2)
				continue
			}

		}

	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}
	loadTuning()
	var rdbs = redis.NewClient(&redis.Options{
		Addr: "redis.postgre-db.svc.cluster.local:6379",
	})
	rdb = rdbs
	ensureWeightValues()

	go pushClientMetricsLoop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startRequestWorker(ctx, "http://"+os.Getenv("LAMINAR_BALANCER_HOST")+":"+os.Getenv("LAMINAR_BALANCER_PORT")+"/balance")
	go StartWorker()

	http.HandleFunc("/hello10", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&currentRate, 10)
		w.Write([]byte("rate set to 10 req/s"))
	})

	http.HandleFunc("/hello100", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&currentRate, 100)
		w.Write([]byte("rate set to 100 req/s"))
	})

	http.HandleFunc("/hello1000", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&currentRate, 1000)
		w.Write([]byte("rate set to 1000 req/s"))
	})

	log.Println("Controller running at :3000")
	log.Fatal(http.ListenAndServe(":3000", nil))
}
