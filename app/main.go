package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Initialize allowed domains (must be after .env load)
	handlers.InitAllowedDomains()

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

	// Start resize worker pool (reads WORKERS env, default 5)
	handlers.StartWorkerPool(0)

	mux := http.NewServeMux()

	mux.HandleFunc("/", handlers.HomeHandler)
	mux.HandleFunc("/i", handlers.ImageInfoHandler)
	mux.HandleFunc("/r/", handlers.ResizeHandler)
	mux.HandleFunc("/resize", handlers.ResizeHandler)
	mux.HandleFunc("/demo", handlers.DemoHandler)
	mux.HandleFunc("/c", handlers.BasicAuth(handlers.ConfigHandler))
	mux.HandleFunc("/config", handlers.BasicAuth(handlers.ConfigHandler))
	mux.HandleFunc("/config/toggle-domain", handlers.BasicAuth(handlers.ToggleDomainHandler))
	mux.HandleFunc("/config/clear-cache", handlers.BasicAuth(handlers.ClearCacheHandler))
	mux.HandleFunc("/config/delete-cache-item", handlers.BasicAuth(handlers.DeleteCacheItemHandler))
	mux.HandleFunc("/cache", handlers.BasicAuth(handlers.CacheExplorerHandler))
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

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Printf("Server starting on http://localhost:%s\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-done
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	// Close databases
	if database.DB != nil {
		database.DB.Close()
	}
	if database.RefererDB != nil {
		database.RefererDB.Close()
	}

	log.Println("Server stopped")
}
