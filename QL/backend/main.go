package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type BackendType struct {
	ID    string
	Redis *redis.Client
	Beta  float64
}

var Backend BackendType = BackendType{
	ID:   os.Getenv("BACKEND_ID"),
	Beta: 0.1,
	Redis: redis.NewClient(&redis.Options{
		Addr: os.Getenv("LAMINAR_REDIS_HOST"),
	}),
}

func (b *BackendType) HandleRequest(w http.ResponseWriter, r *http.Request) {
	jobID := uuid.New().String()
	arrival := time.Now()

	// enqueue job
	b.addJob(jobID, arrival)

	// giả lập SQL query
	execTime := time.Duration(rand.Intn(300)+200) * time.Millisecond
	time.Sleep(execTime)

	// job finished
	wait := time.Since(arrival).Seconds()
	b.finishJob(jobID, wait)

	w.Write([]byte("OK"))
}
func (b *BackendType) addJob(jobID string, arrival time.Time) {
	ctx := context.Background()

	b.Redis.HSet(ctx,
		"node:"+b.ID+":jobs",
		jobID,
		0, // CPU age (seconds)
	)
	b.Redis.Incr(ctx, "node:"+b.ID+":q")
}
func (b *BackendType) finishJob(jobID string, wait float64) {
	ctx := context.Background()

	emaKey := "node:" + b.ID + ":ema"
	prev, _ := b.Redis.Get(ctx, emaKey).Float64()

	ema := (1-b.Beta)*prev + b.Beta*wait
	b.Redis.Set(ctx, emaKey, ema, 0)

	b.Redis.HDel(ctx, "node:"+b.ID+":jobs", jobID)
	b.Redis.Decr(ctx, "node:"+b.ID+":q")
}
func (b *BackendType) AgeUpdater() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		ctx := context.Background()
		jobs, _ := b.Redis.HGetAll(ctx, "node:"+b.ID+":jobs").Result()

		for id, ageStr := range jobs {
			age, _ := strconv.ParseFloat(ageStr, 64)
			age += 0.02 // 20ms
			b.Redis.HSet(ctx, "node:"+b.ID+":jobs", id, age)
		}
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}

	// Re-assign ID after loading env, because global var init happened before load
	if id := os.Getenv("BACKEND_ID"); id != "" {
		Backend.ID = id
	}

	http.HandleFunc("/query", Backend.HandleRequest)
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong backend: " + Backend.ID))
	})

	go Backend.AgeUpdater()

	// Register to Balancer
	ctx := context.Background()
	Backend.Redis.SAdd(ctx, "nodes:active", Backend.ID)
	log.Println("Registered backend:", Backend.ID)

	log.Println("Backend", Backend.ID, "listening on :5000")
	log.Fatal(http.ListenAndServe(":5000", nil))

}
