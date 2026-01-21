package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var (
	A float64 = 0.8
	B float64 = 0.1
	C float64 = 0.1
	e float64 = 0.000001
)

var rdb = redis.NewClient(&redis.Options{
	Addr: "localhost:6379",
})

type VPStype struct {
	IP         string
	FreeQueue  int
	TotalQueue int
	Ra         float64
	Ramax      float64
	PCT        float64
}

var VPS = []VPStype{
	{IP: "1", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "2", FreeQueue: 100, TotalQueue: 100, Ra: 90, Ramax: 120, PCT: 0},
	{IP: "3", FreeQueue: 100, TotalQueue: 100, Ra: 80, Ramax: 120, PCT: 0},
	{IP: "4", FreeQueue: 100, TotalQueue: 100, Ra: 70, Ramax: 120, PCT: 0},
	{IP: "5", FreeQueue: 100, TotalQueue: 100, Ra: 60, Ramax: 120, PCT: 0},
	{IP: "6", FreeQueue: 100, TotalQueue: 100, Ra: 50, Ramax: 120, PCT: 0},
	{IP: "7", FreeQueue: 100, TotalQueue: 100, Ra: 40, Ramax: 120, PCT: 0},
	{IP: "8", FreeQueue: 100, TotalQueue: 100, Ra: 30, Ramax: 120, PCT: 0},
	{IP: "9", FreeQueue: 100, TotalQueue: 100, Ra: 20, Ramax: 120, PCT: 0},
	{IP: "10", FreeQueue: 100, TotalQueue: 100, Ra: 10, Ramax: 120, PCT: 0},
	{IP: "11", FreeQueue: 100, TotalQueue: 100, Ra: 5, Ramax: 120, PCT: 0},
	{IP: "12", FreeQueue: 100, TotalQueue: 100, Ra: 1, Ramax: 120, PCT: 0},
	{IP: "13", FreeQueue: 100, TotalQueue: 100, Ra: 0, Ramax: 120, PCT: 0},
	{IP: "14", FreeQueue: 100, TotalQueue: 100, Ra: 110, Ramax: 120, PCT: 0},
	{IP: "15", FreeQueue: 100, TotalQueue: 100, Ra: 95, Ramax: 120, PCT: 0},
	{IP: "16", FreeQueue: 100, TotalQueue: 100, Ra: 85, Ramax: 120, PCT: 0},
	{IP: "17", FreeQueue: 100, TotalQueue: 100, Ra: 75, Ramax: 120, PCT: 0},
	{IP: "18", FreeQueue: 100, TotalQueue: 100, Ra: 65, Ramax: 120, PCT: 0},
	{IP: "19", FreeQueue: 100, TotalQueue: 100, Ra: 55, Ramax: 120, PCT: 0},
	{IP: "20", FreeQueue: 100, TotalQueue: 100, Ra: 45, Ramax: 120, PCT: 0},
	{IP: "21", FreeQueue: 100, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "22", FreeQueue: 100, TotalQueue: 100, Ra: 90, Ramax: 120, PCT: 0},
	{IP: "23", FreeQueue: 100, TotalQueue: 100, Ra: 80, Ramax: 120, PCT: 0},
	{IP: "24", FreeQueue: 100, TotalQueue: 100, Ra: 70, Ramax: 120, PCT: 0},
	{IP: "25", FreeQueue: 100, TotalQueue: 100, Ra: 60, Ramax: 120, PCT: 0},
	{IP: "26", FreeQueue: 100, TotalQueue: 100, Ra: 50, Ramax: 120, PCT: 0},
	{IP: "27", FreeQueue: 100, TotalQueue: 100, Ra: 40, Ramax: 120, PCT: 0},
	{IP: "28", FreeQueue: 100, TotalQueue: 100, Ra: 30, Ramax: 120, PCT: 0},
	{IP: "29", FreeQueue: 100, TotalQueue: 100, Ra: 20, Ramax: 120, PCT: 0},
	{IP: "30", FreeQueue: 100, TotalQueue: 100, Ra: 10, Ramax: 120, PCT: 0},
	{IP: "31", FreeQueue: 100, TotalQueue: 100, Ra: 5, Ramax: 120, PCT: 0},
	{IP: "32", FreeQueue: 100, TotalQueue: 100, Ra: 1, Ramax: 120, PCT: 0},
	{IP: "33", FreeQueue: 100, TotalQueue: 100, Ra: 0, Ramax: 120, PCT: 0},
	{IP: "34", FreeQueue: 100, TotalQueue: 100, Ra: 110, Ramax: 120, PCT: 0},
	{IP: "35", FreeQueue: 100, TotalQueue: 100, Ra: 95, Ramax: 120, PCT: 0},
	{IP: "36", FreeQueue: 100, TotalQueue: 100, Ra: 85, Ramax: 120, PCT: 0},
	{IP: "37", FreeQueue: 100, TotalQueue: 100, Ra: 75, Ramax: 120, PCT: 0},
	{IP: "38", FreeQueue: 100, TotalQueue: 100, Ra: 65, Ramax: 120, PCT: 0},
	{IP: "39", FreeQueue: 100, TotalQueue: 100, Ra: 55, Ramax: 120, PCT: 0},
	{IP: "40", FreeQueue: 100, TotalQueue: 100, Ra: 45, Ramax: 120, PCT: 0},
}

func setRedis() {

	ctx := context.Background()

	for _, vps := range VPS {
		b, err := json.Marshal(vps)
		err = rdb.Set(
			ctx,
			"vps:"+vps.IP,
			b,
			time.Hour, // TTL = 1 giờ
		).Err()
		if err != nil {
			panic(err)
		}
	}
}
func UpdateRedis(vps []VPStype) {
	ctx := context.Background()

	b, err := json.Marshal(vps)
	if err != nil {
		panic(err)
	}

	err = rdb.Set(
		ctx,
		"vps:"+vps[0].IP,
		b,
		time.Hour, // TTL = 1 hour
	).Err()
	if err != nil {
		panic(err)
	}
}
func GetListVPS() []VPStype {
	var server []VPStype
	ctx := context.Background()

	keys, _ := rdb.Keys(ctx, "vps:*").Result()

	for _, k := range keys {
		b, err := rdb.Get(ctx, k).Bytes()
		if err != nil {
			continue
		}

		var v VPStype
		json.Unmarshal(b, &v)
		server = append(server, v)
	}
	return server
}

func calculatePnew(Pold float64, Pnew float64) ([]VPStype, error) {

	var CP float64
	var VPSS []VPStype
	server := GetListVPS()
	for _, server := range server {
		if Pold == 0 {
			Pold = 1
		}

		Pnew = Pold*0.7 + Pnew*0.3
		CP = (Pnew - Pold) / (Pold + e)

		var QP float64
		QP = 1.0 - float64(server.FreeQueue)/float64(server.TotalQueue)

		var SR = server.Ra / server.Ramax
		var PCT float64
		PCT = (A * CP) + (B * QP) - (C * SR)
		if PCT < 0 {
			server.PCT = PCT
		}
		VPSS = append(VPSS, server)
	}
	sort.Slice(VPSS, func(i, j int) bool {
		return VPSS[i].PCT < VPSS[j].PCT
	})
	var best []VPStype
	n := len(VPSS)
	if n == 0 {
		panic("no backend available")
	}

	if n == 1 {
		best = append(best, VPSS[0])
	} else {
		i := rand.Intn(n)
		j := rand.Intn(n - 1)
		if j >= i {
			j++
		}

		v1 := VPSS[i]
		v2 := VPSS[j]

		// power-of-two: chọn backend ít áp lực hơn
		if v1.PCT <= v2.PCT {
			best = append(best, v1)
		} else {
			best = append(best, v2)
		}

		fmt.Println("Fallback (Po2):", v1.IP, "vs", v2.IP, "->", best[0].IP)
	}
	return best, nil

}
func DeleteAllRedis() error {
	ctx := context.Background()
	return rdb.Do(ctx, "FLUSHALL", "ASYNC").Err()
}
func HasAnyKey() (bool, error) {
	ctx := context.Background()

	keys, _, err := rdb.Scan(ctx, 0, "*", 1).Result()
	if err != nil {
		return false, err
	}

	return len(keys) > 0, nil
}

func main() {
	err := DeleteAllRedis()
	if err != nil {
		fmt.Println("Lỗi khi xóa dữ liệu redis:", err)
		panic(err)
	}
	setRedis()
	_, err = HasAnyKey()
	if err != nil {
		fmt.Println("Lỗi khi kiểm tra key trong redis:", err)
		panic(err)
	} else {
		fmt.Println("Đã có key trong redis")
	}

	router := gin.Default()
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong load balancer",
		})
	})

	router.POST("/balance", func(c *gin.Context) {
		var jsonData map[string]interface{}

		if err := c.BindJSON(&jsonData); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		server, err := calculatePnew(0, 0)
		if err != nil {
			fmt.Println("Lỗi khi tính toán Pnew:", err)
			c.JSON(500, gin.H{"error": "Internal Server Error"})
			return
		}
		URL := "http://" + server[0].IP + ":8080/process"
		jsonData["backend_url"] = URL

		req, err := http.NewRequest(http.MethodPost, URL, nil)
		if err != nil {
			fmt.Println("Lỗi khi tạo request:", err)
			c.JSON(500, gin.H{"error": "Internal Server Error"})
			return
		}
		_ = req

		// Process the jsonData as needed
		fmt.Printf("Received JSON From Backend: %v\n", req.Body)

		c.JSON(200, gin.H{
			"status": "success",
			"data":   req.Body,
		})
	})
	router.Run(":8085")
}
