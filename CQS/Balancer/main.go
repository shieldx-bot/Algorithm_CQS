package main

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

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
