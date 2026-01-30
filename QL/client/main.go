package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	currentRate int64 = 1 // req/s mặc định
	rdb         *redis.Client
	latencyChan = make(chan float64, 10000) // miliseconds
)

func init() {
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
}

// updateMinMax helper to update min/max in Redis
func updateMinMax(ctx context.Context, keyPrefix string, val float64) {
	minKey := keyPrefix + ":min"
	maxKey := keyPrefix + ":max"

	// Update Max
	// Lua script to retrieve current max and update if new val is greater
	// Or simpler: GET -> Compare -> SET (Optimistic locking or just simple since strictly increasing max is monotonic usually, but here p95 fluctuates)
	// For "tài nguyên sử dụng ít", we can just fetch and update.
	// To be correct with concurrency, Lua is best, but let's stick to simple Go logic for readability unless requested otherwise.
	// Actually for "Min/Max of P95", we want the global extrema.

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
	if currMinStr == "" || (val < currMin && val > 0) { // assuming 0 is invalid/empty or we want positive latency
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

	// Đảm bảo ticker được khởi tạo
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

func sendRequest() {
	start := time.Now()
	// Gửi request tới Balancer
	resp, err := http.Get("http://localhost:8090/query")
	latency := float64(time.Since(start).Milliseconds())

	if err != nil {
		log.Println("Request error:", err)
		return
	}

	// Push latency to channel (non-blocking if full)
	select {
	case latencyChan <- latency:
	default:
	}

	defer resp.Body.Close()
	io.ReadAll(resp.Body) // Đọc hết body để tái sử dụng connection
}

func main() {
	// Start background worker sending requests
	go requestWorker()
	// Start metrics worker
	go metricsWorker()

	// Control endpoints
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
