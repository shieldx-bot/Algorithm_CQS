package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

var (
	balancerURL = "http://localhost:8084"
	myIP        = "localhost"
	queueSize   int64
)

func main() {
	if v := os.Getenv("BALANCER_URL"); v != "" {
		balancerURL = v
	}
	if v := os.Getenv("MY_IP"); v != "" {
		myIP = v
	}
	port := "8081"
	if v := os.Getenv("PORT"); v != "" {
		port = v
	}

	http.HandleFunc("/TestHTTP3", handleRequest)

	log.Printf("Backend listening on :%s, reporting to %s as %s", port, balancerURL, myIP)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&queueSize, 1)
	start := time.Now()
	defer atomic.AddInt64(&queueSize, -1)

	// Simulate work
	// giả lập SQL query
	execTime := time.Duration(rand.Intn(300)+200) * time.Millisecond
	time.Sleep(execTime)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Processed"))

	doneTime := time.Since(start).Milliseconds()
	go sendMetrics(doneTime)
}

func sendMetrics(doneTimeMs int64) {
	payload := map[string]interface{}{
		"TimeDoneTask":   doneTimeMs,
		"TimeStartSend":  time.Now().UnixMilli(), // Approximate
		"Penumj":         0,
		"Pemips":         0,
		"NumberTask":     1,
		"ttj":            0.0,
		"tli":            0,
		"ip_vm":          myIP,
		"ifs":            0,
		"vmbw":           0.0,
		"total_on_queue": atomic.LoadInt64(&queueSize),
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(balancerURL+"/receive-metrics", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to send metrics: %v", err)
		return
	}
	defer resp.Body.Close()
}
