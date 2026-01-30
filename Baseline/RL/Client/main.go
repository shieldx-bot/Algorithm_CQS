package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	balancerURL := "http://localhost:8084/load-test-http3"
	if v := os.Getenv("BALANCER_URL"); v != "" {
		balancerURL = v
	}

	ticker := time.NewTicker(100 * time.Millisecond) // 10 RPS
	defer ticker.Stop()

	log.Printf("Client starting, target: %s", balancerURL)

	for range ticker.C {
		go sendRequest(balancerURL)
	}
}

func sendRequest(url string) {
	resp, err := http.Post(url, "text/plain", bytes.NewBufferString("hello"))
	if err != nil {
		log.Printf("Request failed: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	// log.Printf("Response: %s", resp.Status)
}
