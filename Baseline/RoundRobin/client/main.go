package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

var (
	currentRate int64 = 10 // req/s mặc định
)

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
	req, err := http.NewRequest("POST", url, bytes.NewReader(PayloadRequest))
	if err != nil {
		log.Println("new request error:", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("request error:", err)
		return
	}
	defer resp.Body.Close()
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startRequestWorker(ctx, "http://localhost:8085/balance")

	http.HandleFunc("/helo", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&currentRate, 10)
		w.Write([]byte("rate set to 10 req/s"))
	})

	http.HandleFunc("/halo", func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&currentRate, 100)
		w.Write([]byte("rate set to 100 req/s"))
	})

	log.Println("Controller running at :3000")
	log.Fatal(http.ListenAndServe(":3000", nil))
}
