package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"google.golang.org/protobuf/types/known/structpb"
)

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
func main() {
	router := gin.Default()
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}
	var host_database = os.Getenv("LAMINAR_DB_HOST")
	var port_database = os.Getenv("LAMINAR_DB_PORT")
	var user_database = os.Getenv("LAMINAR_DB_USER")
	var password_database = os.Getenv("LAMINAR_DB_PASSWORD")
	var name_database = os.Getenv("LAMINAR_DB_NAME")

	fmt.Println("Starting Laminar Proxy Server...")
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", host_database, port_database, user_database, password_database, name_database)
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
		Query string `json:"query"`
	}
	router.POST("/process", func(c *gin.Context) {
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

		fmt.Printf("Received JSON: %v\n", jsonData)
		c.JSON(200, gin.H{
			"status": "processed",
			"data":   results,
		})
	})
	router.Run(":5000")
}
