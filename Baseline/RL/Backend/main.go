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

	"github.com/joho/godotenv"
)

var (
	balancerURL = "http://balancer.balancer-ns.svc.cluster.local:8085"
	myIP        = os.Getenv("BACKEND_ID")
	queueSize   int64
)

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}
	if v := os.Getenv("BALANCER_URL"); v != "" {
		balancerURL = v
	} else {
		host := os.Getenv("LAMINAR_BALANCER_HOST")
		port := os.Getenv("LAMINAR_BALANCER_PORT")
		if host != "" && port != "" {
			balancerURL = "http://" + host + ":" + port
		}
	}
	if v := os.Getenv("BACKEND_ID"); v != "" {
		myIP = v
	}

	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	})
	http.HandleFunc("/TestHTTP3", handleRequest)

	log.Printf("Backend listening on :5000, reporting to %s as %s", balancerURL, myIP)
	log.Fatal(http.ListenAndServe(":5000", nil))
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
