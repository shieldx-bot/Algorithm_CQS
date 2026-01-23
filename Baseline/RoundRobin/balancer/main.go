package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type Server struct {
	IP string
}

var server = []Server{
	{IP: "localhost:6001"},
	{IP: "localhost:5000"},
}

var rrCounter uint64

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

	start := int(atomic.AddUint64(&rrCounter, 1)-1) % len(server)
	ctx := c.Request.Context()

	var lastErr error
	for i := 0; i < len(server); i++ {
		idx := (start + i) % len(server)
		backend := server[idx].IP

		status, body, err := forwardToBackend(ctx, backend, payload)
		if err != nil {
			lastErr = err
			continue
		}

		c.JSON(status, gin.H{
			"status":   "success",
			"server":   backend,
			"response": string(body),
		})
		return
	}

	if lastErr != nil {
		c.JSON(502, gin.H{"status": "error", "error": lastErr.Error()})
		return
	}
	c.JSON(502, gin.H{"status": "error", "error": "no available servers"})
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

	_ = router.Run(":8085")
}
