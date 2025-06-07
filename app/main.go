package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"image-resize/app/database"
	"image-resize/app/handlers"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	// Initialize database
	if err := database.InitDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	// Initialize referer tracking database
	if err := database.InitRefererDB(); err != nil {
		log.Fatal("Failed to initialize referer database:", err)
	}

	// Start database cleanup service
	database.StartCleanupService()

	mux := http.NewServeMux()

	mux.HandleFunc("/", handlers.HomeHandler)
	mux.HandleFunc("/i", handlers.ImageInfoHandler)
	mux.HandleFunc("/r", handlers.ResizeHandler)
	mux.HandleFunc("/c", handlers.ConfigHandler)
	mux.HandleFunc("/config/toggle-domain", handlers.ToggleDomainHandler)
	mux.HandleFunc("/favicon.ico", handlers.FaviconHandler)

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Server starting on http://localhost:%s\n", port)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
