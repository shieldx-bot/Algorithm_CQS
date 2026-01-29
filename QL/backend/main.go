package main

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Backend struct {
	ID    string
	Redis *redis.Client
	Beta  float64
}

func (b *Backend) HandleRequest(w http.ResponseWriter, r *http.Request) {
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
func (b *Backend) addJob(jobID string, arrival time.Time) {
	ctx := context.Background()

	b.Redis.HSet(ctx,
		"node:"+b.ID+":jobs",
		jobID,
		0, // CPU age (seconds)
	)
	b.Redis.Incr(ctx, "node:"+b.ID+":q")
}
func (b *Backend) finishJob(jobID string, wait float64) {
	ctx := context.Background()

	emaKey := "node:" + b.ID + ":ema"
	prev, _ := b.Redis.Get(ctx, emaKey).Float64()

	ema := (1-b.Beta)*prev + b.Beta*wait
	b.Redis.Set(ctx, emaKey, ema, 0)

	b.Redis.HDel(ctx, "node:"+b.ID+":jobs", jobID)
	b.Redis.Decr(ctx, "node:"+b.ID+":q")
}
func (b *Backend) AgeUpdater() {
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

}
