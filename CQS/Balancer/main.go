package main

import (
	"fmt"
	"sort"

	"github.com/gin-gonic/gin"
)

var (
	A float64 = 0.8
	B float64 = 0.1
	C float64 = 0.1
	e float64 = 0.000001
)

type VPStype struct {
	IP         string
	FreeQueue  int
	TotalQueue int
	Ra         float64
	Ramax      float64
	PCT        float64
}

var VPS = []VPStype{
	{IP: "1", FreeQueue: 10, TotalQueue: 100, Ra: 100, Ramax: 120, PCT: 0},
	{IP: "2", FreeQueue: 70, TotalQueue: 100, Ra: 90, Ramax: 120, PCT: 0},
	{IP: "3", FreeQueue: 60, TotalQueue: 100, Ra: 80, Ramax: 120, PCT: 0},
}

func calculatePnew(Pold float64, Pnew float64) string {

	var CP float64
	var VPSS []VPStype
	for _, server := range VPS {
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
	var best VPStype
	best = VPSS[0]
	return best.IP

}

func main() {
	fmt.Println("Hello, World!")
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

		// Process the jsonData as needed
		fmt.Printf("Received JSON: %v\n", jsonData)

		c.JSON(200, gin.H{
			"status": "success",
			"data":   jsonData,
		})
	})
	router.Run(":8085")
}
