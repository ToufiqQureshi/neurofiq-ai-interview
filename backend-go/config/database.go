package config

import (
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DB is the global database instance we will use across our app
var DB *gorm.DB

// ConnectDB initializes the connection to Supabase
func ConnectDB() {
	// Read the connection string we set in .env
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	// Open the connection using GORM and the Postgres driver
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("Database connected successfully via GORM!")
}
