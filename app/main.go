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
	// Initialize log capturing before anything else
	handlers.InitLogCapture()
	
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
	mux.HandleFunc("/r/", handlers.ResizeHandler)
	mux.HandleFunc("/resize", handlers.ResizeHandler)
	mux.HandleFunc("/demo", handlers.DemoHandler)
	mux.HandleFunc("/c", handlers.BasicAuth(handlers.ConfigHandler))
	mux.HandleFunc("/config", handlers.BasicAuth(handlers.ConfigHandler))
	mux.HandleFunc("/config/toggle-domain", handlers.BasicAuth(handlers.ToggleDomainHandler))
	mux.HandleFunc("/logs", handlers.BasicAuth(handlers.LogsHandler))
	mux.HandleFunc("/ws/logs", handlers.LogsWebSocketHandler)
	mux.HandleFunc("/favicon.ico", handlers.FaviconHandler)
	
	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

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
