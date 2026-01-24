package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

var (
	currentRate int64   = 2 // req/s mặc định
	CPnew       float64 = 1
	CPold       float64 = 1
	rdb         *redis.Client
	Apsilon     float64 = 0.05
	LatencyOld  float64 = 1
	LatencyNew  float64 = 1
	emaAlpha    float64 = 0.2
	cpEma       float64 = 1
	latencyEma  float64 = 1
)

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
		log.Println("request error:", err)
		return
	}
	fmt.Printf("Response status code: %d\n", resp)
	TimeNow := time.Now()
	TailLatency := TimeNow.Sub(payload.TimeStart).Milliseconds()
	LatencyNew = float64(TailLatency)
	latencyRatio := LatencyNew / (LatencyOld + 0.0001)
	LatencyOld = LatencyNew
	latencyEma = (emaAlpha * latencyRatio) + ((1 - emaAlpha) * latencyEma)
	JobChannel <- Job{Action: latencyEma, Type: "Latency"}

	defer resp.Body.Close()
}

func updateWeight(field string, factor float64) {
	val, err := rdb.HGet(
		context.Background(),
		"key:weightValue",
		field,
	).Float64()
	if err != nil {
		panic(err)
	}
	val = val * factor
	if val < 0.1 {
		val = 0.1
	} else if val > 1 {
		val = 1
	}
	if err := rdb.HSet(
		context.Background(),
		"key:weightValue",
		field,
		val,
	).Err(); err != nil {
		panic(err)
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
	var rdbs = redis.NewClient(&redis.Options{
		Addr: "redis.postgre-db.svc.cluster.local:6379",
	})
	rdb = rdbs

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
