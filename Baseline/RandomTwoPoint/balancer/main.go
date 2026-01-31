package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	IP string
}

var server = []Server{
	{IP: "backend.backend-ns.svc.cluster.local:5000"},
	{IP: "backend2.backend2-ns.svc.cluster.local:5000"},
	{IP: "backend3.backend3-ns.svc.cluster.local:5000"},
	{IP: "backend4.backend4-ns.svc.cluster.local:5000"},
	{IP: "backend5.backend5-ns.svc.cluster.local:5000"},
	{IP: "backend6.backend6-ns.svc.cluster.local:5000"},
	{IP: "backend7.backend7-ns.svc.cluster.local:5000"},
	{IP: "backend8.backend8-ns.svc.cluster.local:5000"},
	{IP: "backend9.backend9-ns.svc.cluster.local:5000"},
	{IP: "backend10.backend10-ns.svc.cluster.local:5000"},
}

var rdb *redis.Client

// Store queue sizes locally for fast lookups in Random Two Point
var (
	queueSizesMu sync.RWMutex
	queueSizes   = map[string]int64{}
)

func init() {
	redisHost := os.Getenv("LAMINAR_REDIS_HOST")
	redisPort := os.Getenv("LAMINAR_REDIS_PORT")

	var redisAddr string
	if redisHost == "" {
		redisAddr = "redis.postgre-db.svc.cluster.local:6379"
	} else {
		// remove redis:// prefix if present
		host := strings.TrimPrefix(redisHost, "redis://")
		if redisPort == "" {
			redisPort = "6379"
		}
		redisAddr = host + ":" + redisPort
	}
	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// Initialize random seed
	rand.Seed(time.Now().UnixNano())
}

type Frequency struct {
	Count int64     // internal counter
	Time  time.Time // last updated time
	Pold  float64   // previous count
	Pnew  float64   // new count
}

type BalanceRequest struct {
	Payload   Frequency `json:"payload"`
	TimeStart time.Time `json:"time_start"`
	Query     string    `json:"query,omitempty"`
}

func forwardToBackend(ctx context.Context, backend string, payload []byte) (int, []byte, error) {
	url := "http://" + backend + "/process"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// Random Two Point Selection Logic
func selectBackendRandomTwoPoint() string {
	if len(server) == 0 {
		return ""
	}
	if len(server) == 1 {
		return server[0].IP
	}

	// Pick two distinct random indices
	idx1 := rand.Intn(len(server))
	idx2 := rand.Intn(len(server))
	// Try to get a different index
	for i := 0; i < 5 && idx1 == idx2; i++ {
		idx2 = rand.Intn(len(server))
	}

	ip1 := server[idx1].IP
	ip2 := server[idx2].IP

	queueSizesMu.RLock()
	q1 := queueSizes[ip1]
	q2 := queueSizes[ip2]
	queueSizesMu.RUnlock()

	// Pick the one with smaller queue size
	if q1 <= q2 {
		return ip1
	}
	return ip2
}

func handleBalance(c *gin.Context) {
	if len(server) == 0 {
		c.JSON(503, gin.H{"status": "error", "error": "no backends configured"})
		return
	}

	var jsonData BalanceRequest
	if err := c.ShouldBindJSON(&jsonData); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	payload, err := json.Marshal(jsonData)
	if err != nil {
		c.JSON(500, gin.H{"error": "Internal Server Error"})
		return
	}
	fmt.Printf("Received JSON: %v\n", jsonData)

	ctx := c.Request.Context()

	// Try multiple times if selected backend fails
	for attempt := 0; attempt < 3; attempt++ {
		backend := selectBackendRandomTwoPoint()
		if backend == "" {
			break
		}

		// Thống kê số lượng request tới mỗi backend (async)
		go func(targetIP string) {
			rdb.Incr(context.Background(), "node:"+targetIP+":selected")
		}(backend)

		status, body, err := forwardToBackend(ctx, backend, payload)
		if err == nil {
			c.JSON(status, gin.H{
				"status":   "success",
				"server":   backend,
				"response": string(body),
			})
			return
		}
		// Log error and retry
		fmt.Printf("Failed to contact %s: %v\n", backend, err)
	}

	c.JSON(502, gin.H{"status": "error", "error": "no available servers or all retries failed"})
}

func main() {
	router := gin.Default()

	// Tolerate accidental whitespace in URL paths (e.g. "/ping ").
	router.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		trimmed := strings.TrimSpace(p)
		if trimmed != p {
			c.Request.URL.Path = trimmed
		}
		c.Next()
	})

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	// Support both routes to match Postman usage.
	router.POST("/balance", handleBalance)

	router.POST("/receive-metrics", func(c *gin.Context) {
		type metricsIn struct {
			TimeDoneTask int64  `json:"TimeDoneTask"`
			IPVM         string `json:"ip_vm"`
			TotalOnQueue int64  `json:"total_on_queue"`
		}

		var in metricsIn
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "invalid json"})
			return
		}
		ip := strings.TrimSpace(in.IPVM)
		if ip != "" {
			ctx := context.Background()

			// Update local map for Random Two Point logic
			queueSizesMu.Lock()
			queueSizes[ip] = in.TotalOnQueue
			queueSizesMu.Unlock()

			// Write Queue Size to Redis for visualization
			rdb.Set(ctx, "node:"+ip+":q", in.TotalOnQueue, 0)
		}

		c.JSON(200, gin.H{"status": "ok"})
	})

	_ = router.Run(":8085")
}
