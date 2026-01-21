package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github/shieldx-bot/H_BAC/metrix"

	"io"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq" // Driver postgres
	"golang.org/x/sync/singleflight"
)

var testHTTP3SingleFlight singleflight.Group
var queryCache *ristretto.Cache

type Job struct {
	Ctx    context.Context
	Metrix map[string]interface{}
}

var (
	jobChan = make(chan *Job, 1000)
	store   = make([]map[string]interface{}, 0, 10000)
)
var TotalOnQueue int64 = 0

func startWorker() {
	go func() {
		for job := range jobChan {
			// Don't store indefinitely, it leaks memory if not used
			// store = append(store, job.Metrix)

			// REMOVE simulated delay. Metrics must be sent ASAP for real-time LB.
			// time.Sleep(5000 * time.Millisecond)

			body, err := json.Marshal(job.Metrix)
			if err != nil {
				continue
			}

			// Use a shorter timeout for metric sending
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://34.87.132.91:8083/receive-metrics", bytes.NewReader(body))
			if err != nil {
				cancel()
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			cancel()
		}
	}()
}

func main() {
	startWorker()

	router := gin.Default()

	// POST query endpoint (good for load tests; avoids any accidental intermediary caching)
	router.POST("/TestHTTP3", func(c *gin.Context) {
		var jsonReq struct {
			QueryId  string `json:"QueryId"`
			QuerySQL string `json:"QuerySQL"`
			Payload  string `json:"Payload"`
		}

		// 1. Bind JSON failure -> return immediately (no metrics)
		if err := c.BindJSON(&jsonReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// --- START METRICS & QUEUE TRACKING ---
		atomic.AddInt64(&TotalOnQueue, 1)
		defer atomic.AddInt64(&TotalOnQueue, -1) // Ensure decrement on exit

		MetrixFirst := metrix.MetrixFirstFunction()
		TimeStart := time.Now()
		var NumberTask int64 = 1

		// Use defer to report metrics regardless of Cache HIT, MISS, or Error
		defer func() {
			MetrixEnd := metrix.MetrixEndFunction()
			TotalTimeTask := time.Since(TimeStart).Milliseconds()
			if TotalTimeTask == 0 {
				TotalTimeTask = 1
			}

			cores := runtime.NumCPU()
			job := &Job{
				Ctx: c.Request.Context(),
				Metrix: map[string]interface{}{
					"TimeStartSend": time.Now().UnixNano() / int64(time.Millisecond),
					"TimeDoneTask":  MetrixEnd.TimeEnd - int64(MetrixFirst.TimeStart), // System time delta
					"Penumj":        cores,
					"Pemips":        MetrixEnd.Pemips,
					"NumberTask":    NumberTask,
					"TTj":           MetrixEnd.TTj,
					"TLi":           TotalTimeTask * int64(MetrixEnd.Pemips), // Workload Estimate
					"IPVM":          c.ClientIP(),
					"TotalOnQueue":  atomic.LoadInt64(&TotalOnQueue),
					"IFS":           MetrixEnd.IFS,
					"VMbw":          MetrixEnd.VMbw,
				},
			}

			// Non-blocking send to jobChan
			select {
			case jobChan <- job:
				// Queued successfully
			default:
				// Queue full - ignore metric but DO NOT fail request
				fmt.Println("Warning: jobChan full, dropping metric")
			}
		}()

		// Preserve per-request QueryId even when coalesced.
		c.JSON(http.StatusOK, gin.H{
			"Status":       " resp.GetStatus()",
			"QueryId":      "  jsonReq.QueryId",
			"Records":      "resp.GetRecords()",
			"ReceivedSize": "resp.GetReceivedSize()",
		})
	})
	router.POST("/http3-proxy", func(c *gin.Context) {
		var reqBody map[string]interface{}
		var NumberTask int64
		MetrixFirst := metrix.MetrixFirstFunction()
		if err := c.BindJSON(&reqBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}
		atomic.AddInt64(&TotalOnQueue, 1)
		defer atomic.AddInt64(&TotalOnQueue, -1)

		TimeStartTask3 := time.Now()
		_ = TimeStartTask3
		// Call HTTP Backedn 



		
		TimeEndTask3 := time.Now()
		TimeDoneTask3 := TimeEndTask3.Sub(TimeStartTask3)
		_ = TimeDoneTask3
		NumberTask += 1

		TotalTimeTask := TimeDoneTask3.Milliseconds()

		MetrixEnd := metrix.MetrixEndFunction()
		cores := runtime.NumCPU()
		job := &Job{
			Ctx: c.Request.Context(),
			Metrix: map[string]interface{}{
				"TimeStartSend": time.Now().UnixNano() / int64(time.Millisecond),
				"TimeDoneTask":  MetrixEnd.TimeEnd - int64(MetrixFirst.TimeStart),
				"Penumj":        cores,
				"Pemips":        MetrixEnd.Pemips,
				"NumberTask":    NumberTask,
				"TTj":           MetrixEnd.TTj,
				"TLi":           TotalTimeTask * int64(MetrixEnd.Pemips),
				"IPVM":          c.ClientIP(),
				"TotalOnQueue":  atomic.LoadInt64(&TotalOnQueue),
				"IFS":           MetrixEnd.IFS,
				"VMbw":          MetrixEnd.VMbw,
			},
		}
		select {
		case jobChan <- job:
			c.JSON(http.StatusOK, gin.H{"status": "queued", "total_jobs": len(store)})
			return
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Server busy"})
		}

	})

	router.POST("/echo", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		time.Sleep(10 * time.Millisecond) // giả lập xử lý
		c.Data(200, "application/json", body)
	})

	port := os.Getenv("LAMINAR_PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	fmt.Printf("Starting server on :%s\n", port)

	router.Run("0.0.0.0:" + port) // listen and serve
}
