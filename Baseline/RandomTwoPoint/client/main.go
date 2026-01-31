package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

var (
	currentRate   int64 = 1 // default req/s
	totalRequests int64 = 0
	rdb           *redis.Client
	latencyChan   = make(chan float64, 10000) // milliseconds
)

type Frequency struct {
	Count int64     // internal counter
	Time  time.Time // last updated time
	Pold  float64   // previous count
	Pnew  float64   // new count
	CP    float64   // current processing rate
}

var Freq = Frequency{Count: 1, Time: time.Now(), Pold: 1, Pnew: 1}

func init() {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis.postgre-db.svc.cluster.local:6379"
	}

	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
}

// updateMinMax helper to update min/max in Redis
func updateMinMax(ctx context.Context, keyPrefix string, val float64) {
	minKey := keyPrefix + ":min"
	maxKey := keyPrefix + ":max"

	// Update Max
	currMaxStr, _ := rdb.Get(ctx, maxKey).Result()
	var currMax float64
	if currMaxStr != "" {
		fmt.Sscanf(currMaxStr, "%f", &currMax)
	}
	if currMaxStr == "" || val > currMax {
		rdb.Set(ctx, maxKey, val, 0)
	}

	// Update Min
	currMinStr, _ := rdb.Get(ctx, minKey).Result()
	var currMin float64
	if currMinStr != "" {
		fmt.Sscanf(currMinStr, "%f", &currMin)
	}
	// Note: if min is 0 or uninitialized, we take the first value
	if currMinStr == "" || (val < currMin && val > 0) {
		rdb.Set(ctx, minKey, val, 0)
	}
}

func metricsWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var samples []float64

	for {
		select {
		case lat := <-latencyChan:
			samples = append(samples, lat)
		case <-ticker.C:
			if len(samples) == 0 {
				continue
			}

			// Copy and reset
			currentSamples := make([]float64, len(samples))
			copy(currentSamples, samples)
			samples = samples[:0]

			go processSamples(currentSamples)
		}
	}
}

func processSamples(samples []float64) {
	if len(samples) == 0 {
		return
	}
	sort.Float64s(samples)
	n := len(samples)

	// Utils
	getVal := func(idx int) float64 {
		if idx >= n {
			idx = n - 1
		}
		return samples[idx]
	}

	minVal := samples[0]
	maxVal := samples[n-1]

	// Avg
	var sum float64
	for _, v := range samples {
		sum += v
	}
	avgVal := sum / float64(n)

	// P90, P95
	p90Val := getVal(int(math.Ceil(float64(n)*0.90)) - 1)
	p95Val := getVal(int(math.Ceil(float64(n)*0.95)) - 1)

	ctx := context.Background()
	updateMinMax(ctx, "client:stats:p90", p90Val)
	updateMinMax(ctx, "client:stats:p95", p95Val)
	updateMinMax(ctx, "client:stats:avg", avgVal)
	updateMinMax(ctx, "client:stats:max", maxVal)
	updateMinMax(ctx, "client:stats:min", minVal)
}

func requestWorker() {
	var ticker *time.Ticker
	var lastRate int64 = 0

	// Ensure ticker is initialized
	if currentRate > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(currentRate))
		lastRate = currentRate
	}

	for {
		rate := atomic.LoadInt64(&currentRate)

		if rate <= 0 {
			if ticker != nil {
				ticker.Stop()
				ticker = nil
			}
			time.Sleep(100 * time.Millisecond)
			lastRate = 0
			continue
		}

		if rate != lastRate {
			if ticker != nil {
				ticker.Stop()
			}
			ticker = time.NewTicker(time.Second / time.Duration(rate))
			lastRate = rate
			log.Printf("Rate changed to: %d req/s", rate)
		}

		if ticker != nil {
			<-ticker.C
			go sendRequest()
		}
	}
}

var balancerURL string

func sendRequest() {
	atomic.AddInt64(&totalRequests, 1)
	start := time.Now()

	// Update Frequency logic for RoundRobin/Base
	if time.Now().Sub(Freq.Time) >= 10*time.Second {
		var temp float64 = Freq.Pnew
		Freq.Pnew = float64(Freq.Pold)*0.7 + float64(Freq.Count)*0.3
		Freq.Pold = temp
		Freq.Count = 0
		Freq.Time = time.Now()
		Freq.CP = (Freq.Pnew - Freq.Pold) / (Freq.Pold + 0.0001)
	} else {
		Freq.Count++
	}

	type Payload struct {
		Payload   Frequency `json:"payload"`
		TimeStart time.Time `json:"time_start"`
		Query     string    `json:"query,omitempty"`
	}
	var payload Payload
	payload.Payload = Freq
	payload.TimeStart = time.Now()

	PayloadRequest, err := json.Marshal(payload)
	if err != nil {
		log.Println("json marshal error:", err)
		return
	}

	// Send POST to balancer
	resp, err := http.Post(balancerURL, "application/json", bytes.NewReader(PayloadRequest))
	latency := float64(time.Since(start).Milliseconds())

	if err != nil {
		log.Println("request error:", err)
		return
	}

	// Push latency to channel (non-blocking if full)
	select {
	case latencyChan <- latency:
	default:
	}

	defer resp.Body.Close()
	io.ReadAll(resp.Body)
}

func runScenario() {
	log.Println("Starting Scenario: Phase 1 (1 req/s for 5m)")
	atomic.StoreInt64(&currentRate, 1)
	time.Sleep(5 * time.Minute)

	log.Println("Starting Scenario: Phase 2 (100 req/s for 5m)")
	atomic.StoreInt64(&currentRate, 100)
	time.Sleep(5 * time.Minute)

	log.Println("Starting Scenario: Phase 3 (1000 req/s until 500k requests)")
	atomic.StoreInt64(&currentRate, 1000)

	for {
		reqs := atomic.LoadInt64(&totalRequests)
		if reqs >= 500000 {
			log.Printf("Reached %d requests. Stopping.", reqs)
			atomic.StoreInt64(&currentRate, 0)
			break
		}
		time.Sleep(1 * time.Second)
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		// log.Println("Error loading .env file")
	}

	balancerURL = "http://balancer.balancer-ns.svc.cluster.local:8085/balance"
	if v := os.Getenv("BALANCER_URL"); v != "" {
		balancerURL = v
	} else {
		host := os.Getenv("LAMINAR_BALANCER_HOST")
		port := os.Getenv("LAMINAR_BALANCER_PORT")
		if host != "" && port != "" {
			balancerURL = "http://" + host + ":" + port + "/balance"
		}
	}

	log.Printf("Client starting, target: %s", balancerURL)

	go runScenario()
	go requestWorker()
	go metricsWorker()

	// Control endpoints
	http.HandleFunc("/_status/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

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

	log.Println("Client Controller listening on :3000")
	log.Fatal(http.ListenAndServe(":3000", nil))
}
