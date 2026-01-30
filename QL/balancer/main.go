package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type LoadBalancer struct {
	Redis  *redis.Client
	Lambda float64
	Eta    float64
	Delta  float64
}

var lb LoadBalancer = LoadBalancer{
	Redis: redis.NewClient(&redis.Options{
		Addr: "redis.postgre-db.svc.cluster.local:6379",
	}),
	Lambda: 0.5,
	Eta:    0.5,
	Delta:  0.1,
}

func (lb *LoadBalancer) HandleClient(w http.ResponseWriter, r *http.Request) {
	backendID := lb.selectBackend()

	// Thống kê số lần chọn backend (chạy async để không chặn request chính)
	go func(id string) {
		lb.Redis.Incr(context.Background(), "node:"+id+":selected")
	}(backendID)

	resp, err := http.Get("http://" + backendID + "/query")
	if err != nil {
		log.Printf("Error contacting backend %s: %v", backendID, err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if resp != nil && resp.Body != nil {
		body, _ := io.ReadAll(resp.Body)
		w.Write(body)
		resp.Body.Close()
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}
func (lb *LoadBalancer) SnapshotNode(id string) (jobs []float64, ema float64) {
	ctx := context.Background()

	jobMap, _ := lb.Redis.HGetAll(ctx, "node:"+id+":jobs").Result()
	for _, ageStr := range jobMap {
		age, _ := strconv.ParseFloat(ageStr, 64)
		jobs = append(jobs, age)
	}

	ema, _ = lb.Redis.Get(ctx, "node:"+id+":ema").Float64()
	return
}
func ComputeCRITFromAges(ages []float64) float64 {
	n := len(ages)
	if n == 0 {
		return 0
	}

	sort.Float64s(ages)

	sumYoung := 0.0
	for i := 0; i < n-1; i++ {
		sumYoung += ages[i]
	}

	aStar := ages[n-1]
	return sumYoung + float64(n-1)/float64(n)*aStar
}
func (lb *LoadBalancer) ComputeState(id string) float64 {
	ages, ema := lb.SnapshotNode(id)

	wcrit := ComputeCRITFromAges(ages)
	what := lb.Lambda*wcrit + (1-lb.Lambda)*ema

	q := len(ages)
	return float64(q) * what
}
func (lb *LoadBalancer) ControlLoop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		ctx := context.Background()
		nodes, _ := lb.Redis.SMembers(ctx, "nodes:active").Result()

		states := make(map[string]float64)
		for _, id := range nodes {
			states[id] = lb.ComputeState(id)
		}

		// normalize X -> routing weight
		var maxX float64
		for _, x := range states {
			if x > maxX {
				maxX = x
			}
		}

		for id, x := range states {
			// weight ∈ [1, 1+γ]
			weight := 1.0
			if maxX > 0 {
				weight += lb.Eta * (x / maxX)
			}
			lb.Redis.Set(ctx, "route:weight:"+id, weight, 0)
		}
	}
}
func (lb *LoadBalancer) selectBackend() string {
	ctx := context.Background()
	nodes, _ := lb.Redis.SMembers(ctx, "nodes:active").Result()

	if len(nodes) == 1 {
		return nodes[0]
	}

	i := rand.Intn(len(nodes))
	j := rand.Intn(len(nodes))
	for j == i {
		j = rand.Intn(len(nodes))
	}

	n1 := nodes[i]
	n2 := nodes[j]

	q1, _ := lb.Redis.Get(ctx, "node:"+n1+":q").Int()
	q2, _ := lb.Redis.Get(ctx, "node:"+n2+":q").Int()

	w1, _ := lb.Redis.Get(ctx, "route:weight:"+n1).Float64()
	w2, _ := lb.Redis.Get(ctx, "route:weight:"+n2).Float64()

	// effective load = q × weight
	e1 := float64(q1) * w1
	e2 := float64(q2) * w2

	if e1 <= e2 {
		return n1
	}
	return n2
}

func main() {

	http.HandleFunc("/query", lb.HandleClient)
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong balancer"))
	})
	go lb.ControlLoop()
	fmt.Println("Load Balancer listening on :8085")

	http.ListenAndServe(":8085", nil)
}
