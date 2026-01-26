package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/structpb"
)

type Job struct {
	Action float64 `json:"action"`
}

var JobChannel = make(chan Job, 1000)
var RaOld float64 = 0.0001
var RaNew float64 = 0.0001
var rdbLocal *redis.Client
var Apsilon float64 = 0.02

var weightCooldown = 45 * time.Second
var lastWeightUpdate time.Time

func loadTuning() {
	if v := os.Getenv("WEIGHT_EPSILON"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 0.2 {
			Apsilon = f
		}
	}
	if v := os.Getenv("WEIGHT_COOLDOWN_SEC"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 5 {
			weightCooldown = time.Duration(s) * time.Second
		}
	}
}

type ExampleRecord struct {
	ID            int    `json:"id"`
	USERNAME      string `json:"username"`
	EMAIL         string `json:"email"`
	PASSWORD_HASH string `json:"password_hash"`
	BALANCE       int64  `json:"balance"`
	IS_ACTIVE     bool   `json:"is_active"`
	CREATED_AT    string `json:"created_at"`
	UPDATED_AT    string `json:"updated_at"`
}

func ExecuteSQLQery(query string, db *sql.DB) ([]*structpb.Struct, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var record []ExampleRecord
	for rows.Next() {
		var r ExampleRecord
		if err := rows.Scan(&r.ID, &r.USERNAME, &r.EMAIL, &r.PASSWORD_HASH, &r.BALANCE, &r.IS_ACTIVE, &r.CREATED_AT, &r.UPDATED_AT); err != nil {
			return nil, err
		}
		record = append(record, r)

	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []*structpb.Struct
	for _, r := range record {
		// structpb only supports JSON-like scalars; normalize ints to float64.
		rowMap := map[string]interface{}{
			"id":            float64(r.ID),
			"username":      r.USERNAME,
			"email":         r.EMAIL,
			"password_hash": r.PASSWORD_HASH,
			"balance":       float64(r.BALANCE),
			"is_active":     r.IS_ACTIVE,
			"created_at":    r.CREATED_AT,
			"updated_at":    r.UPDATED_AT,
		}

		st, err := structpb.NewStruct(rowMap)
		if err != nil {
			return nil, err
		}
		results = append(results, st)
	}

	return results, nil

}

type MetrixBackend struct {
	TotalQueue  int64     `json:"total_queue"`
	FreeQueue   int64     `json:"free_queue"`
	Ra          float64   `json:"ra"`
	Ramax       float64   `json:"ramax"`
	RequestDone int64     `json:"request_done"`
	TimeStart   time.Time `json:"time_start"`
}

type VPStype struct {
	IP         string
	FreeQueue  int
	TotalQueue int
	Ra         float64
	Ramax      float64
	PCT        float64
}

var MetrixFeedBackend = MetrixBackend{
	TotalQueue:  100,
	FreeQueue:   100,
	Ra:          100,
	Ramax:       120,
	RequestDone: 0,
	TimeStart:   time.Now(),
}

var metrixMu sync.RWMutex

func SetRedisMetrix(rdb *redis.Client) {
	ctx := context.Background()

	metrixMu.RLock()
	defer metrixMu.RUnlock()

	err := rdb.HSet(
		ctx,
		"vps:"+os.Getenv("MY_IP_PUBLIC"),
		"TotalQueue", MetrixFeedBackend.TotalQueue,
		"FreeQueue", MetrixFeedBackend.FreeQueue,
		"Ra", MetrixFeedBackend.Ra,
		"Ramax", MetrixFeedBackend.Ramax,
		"RequestDone", MetrixFeedBackend.RequestDone,
	).Err()
	if err != nil {
		panic(err)
	}
}

func UpdateRa() {
	last := time.Now()
	for {
		time.Sleep(10 * time.Second)
		now := time.Now()
		elapsed := now.Sub(last).Seconds()
		if elapsed <= 0 {
			last = now
			continue
		}
		metrixMu.Lock()
		if MetrixFeedBackend.RequestDone == 0 {
			metrixMu.Unlock()
			last = now
			continue
		}
		MetrixFeedBackend.Ra = float64(MetrixFeedBackend.RequestDone) / elapsed
		MetrixFeedBackend.RequestDone = 0
		if MetrixFeedBackend.Ra > MetrixFeedBackend.Ramax {
			MetrixFeedBackend.Ra = MetrixFeedBackend.Ramax
		}
		temp := RaNew
		RaNew = MetrixFeedBackend.Ra
		RaOld = temp

		JobChannel <- Job{Action: RaNew / RaOld}
		metrixMu.Unlock()
		last = now
	}
}

func ActionPushMetrix(rdb *redis.Client) {
	for {
		time.Sleep(10 * time.Second)
		metrixMu.RLock()
		totalQueue := MetrixFeedBackend.TotalQueue
		// freeQueue := MetrixFeedBackend.FreeQueue
		metrixMu.RUnlock()
		if totalQueue == 0 {
			continue
		}
		SetRedisMetrix(rdb)

	}
}

func CheckHealthUpdate(rdb *redis.Client) {
	fmt.Printf("\n========================= START TESTING BACKEND ====================\n")
	fmt.Printf("\n B0: Check exits env \n")
	if os.Getenv("MY_IP_PUBLIC") == "" {
		log.Println("Environment variable MY_IP_PUBLIC is not set")

		return
	} else {
		fmt.Println("MY_IP_PUBLIC:", os.Getenv("MY_IP_PUBLIC"))
		fmt.Println("URL REDIS: ", os.Getenv("LAMINAR_REDIS_HOST")+":"+os.Getenv("LAMINAR_REDIS_PORT"))
	}
	fmt.Printf("\n PASS B0: Check exits env \n")
	fmt.Printf("\n B1: Check healthy redis: \n ")
	res, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Println("Redis ping error:", err)
		return
	}
	log.Println("\n Redis ping response:", res)
	fmt.Printf("\n PASS B1: Check healthy redis \n")

	SetRedisMetrix(rdb)
	fmt.Printf("\n B3: Set initial metrix to redis: \n ")
	val, err := rdb.HGetAll(context.Background(), "vps:"+os.Getenv("MY_IP_PUBLIC")).Result()
	if err != nil {
		log.Println("Redis HGetAll error:", err)
		return
	}
	log.Println("\n Initial metrix from redis:", val)
	fmt.Printf("\n PASS B3: Set initial metrix to redis \n")

}

func StartWorker() {
	for {
		job := <-JobChannel
		_ = job
		if !lastWeightUpdate.IsZero() && time.Since(lastWeightUpdate) < weightCooldown {
			continue
		}
		if job.Action > 1.70 {
			val, err := rdbLocal.HGet(
				context.Background(),
				"key:weightValue",
				"weightC",
			).Float64()

			if err != nil {
				// redis.Nil = chưa tồn tại
				panic(err)
			}
			val = val * (1 + Apsilon)

			err = rdbLocal.HSet(
				context.Background(),
				"key:weightValue",
				"weightC",
				val,
			).Err()
			if err != nil {
				panic(err)
			}
			normalizeWeights()
			lastWeightUpdate = time.Now()
			continue
		} else if job.Action > 1.40 {
			val, err := rdbLocal.HGet(
				context.Background(),
				"key:weightValue",
				"weightC",
			).Float64()

			if err != nil {
				// redis.Nil = chưa tồn tại
				panic(err)
			}
			val = val * (1 + Apsilon/2)

			err = rdbLocal.HSet(
				context.Background(),
				"key:weightValue",
				"weightC",
				val,
			).Err()
			if err != nil {
				panic(err)
			}
			normalizeWeights()
			lastWeightUpdate = time.Now()
			continue
		}
	}
}

func normalizeWeights() {
	ctx := context.Background()
	vals, err := rdbLocal.HGetAll(ctx, "key:weightValue").Result()
	if err != nil {
		return
	}
	a, _ := strconv.ParseFloat(vals["weightA"], 64)
	b, _ := strconv.ParseFloat(vals["weightB"], 64)
	c, _ := strconv.ParseFloat(vals["weightC"], 64)
	a = clamp(a, 0.1, 1)
	b = clamp(b, 0.1, 1)
	c = clamp(c, 0.1, 1)
	sum := a + b + c
	if sum == 0 {
		return
	}
	a /= sum
	b /= sum
	c /= sum
	_ = rdbLocal.HSet(ctx, "key:weightValue", "weightA", a, "weightB", b, "weightC", c).Err()
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}
	loadTuning()
	var rdb = redis.NewClient(&redis.Options{
		Addr: "redis.postgre-db.svc.cluster.local:6379",
	})
	rdbLocal = rdb

	CheckHealthUpdate(rdb)
	fmt.Printf("========================= PASS ALL CASE TEST ====================\n")
	go UpdateRa()
	go ActionPushMetrix(rdb)
	go StartWorker()
	router := gin.Default()

	// var host_database = os.Getenv("LAMINAR_DB_HOST")
	// var port_database = os.Getenv("LAMINAR_DB_PORT")
	// var user_database = os.Getenv("LAMINAR_DB_USER")
	// var password_database = os.Getenv("LAMINAR_DB_PASSWORD")
	// var name_database = os.Getenv("LAMINAR_DB_NAME")

	fmt.Println("Starting Laminar Proxy Server...")
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", "postgres.postgre-db.svc.cluster.local", "5432", "laminar", "Vananh12345", "laminar")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	//
	db.SetMaxOpenConns(200)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		fmt.Println("DB Fail:", err)
	} else {
		fmt.Println("Connected to DB successfully")
	}

	type ProcessRequest struct {
		Query     string    `json:"query"`
		CP        float64   `json:"cp"`
		TimeStart time.Time `json:"time_start"`
	}

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong backend",
		})
	})
	router.POST("/process", func(c *gin.Context) {
		metrixMu.Lock()
		if MetrixFeedBackend.FreeQueue <= 0 {
			metrixMu.Unlock()
			c.JSON(429, gin.H{
				"status": "error",
				"error":  "backend queue full",
			})
			return
		}
		MetrixFeedBackend.FreeQueue--
		metrixMu.Unlock()
		defer func() {
			metrixMu.Lock()
			MetrixFeedBackend.FreeQueue++
			if MetrixFeedBackend.FreeQueue > MetrixFeedBackend.TotalQueue {
				MetrixFeedBackend.FreeQueue = MetrixFeedBackend.TotalQueue
			}
			metrixMu.Unlock()
		}()
		var jsonData ProcessRequest

		if err := c.ShouldBindJSON(&jsonData); err != nil {
			c.JSON(400, gin.H{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}

		results, err := ExecuteSQLQery(jsonData.Query, db)
		if err != nil {
			c.JSON(500, gin.H{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}
		metrixMu.Lock()
		MetrixFeedBackend.RequestDone++
		metrixMu.Unlock()

		fmt.Printf("Received JSON: %v\n", jsonData)
		c.JSON(200, gin.H{
			"status":     "processed",
			"data":       results,
			"time_start": jsonData.TimeStart,
		})
	})
	router.Run(":5000")
}
