package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

// MaxDatabaseSizeMB is the maximum allowed database size in megabytes
var MaxDatabaseSizeMB int

func init() {
	// Read MAX_DB_SIZE from environment or use default
	maxDBSize := os.Getenv("MAX_DB_SIZE")
	if maxDBSize != "" {
		size, err := strconv.Atoi(maxDBSize)
		if err != nil {
			log.Printf("Invalid MAX_DB_SIZE value '%s', using default 100MB", maxDBSize)
			MaxDatabaseSizeMB = 1000
		} else {
			MaxDatabaseSizeMB = size
			log.Printf("Database size limit set to %dMB from MAX_DB_SIZE env", MaxDatabaseSizeMB)
		}
	} else {
		MaxDatabaseSizeMB = 1000
	}
}

func InitDB() error {
	// Create db directory if it doesn't exist
	dbDir := "db"
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create db directory: %w", err)
	}

	// Set database path
	dbPath := filepath.Join(dbDir, "image_cache.db")

	var err error
	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Printf("Database initialized at: %s", dbPath)
	return nil
}

func createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS image_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		width INTEGER,
		original_data BLOB,
		resized_data BLOB,
		content_type TEXT,
		response_format TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(url, width)
	);

	CREATE INDEX IF NOT EXISTS idx_url_width ON image_cache(url, width);
	`

	_, err := DB.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

func GetCachedImage(url string, width int) ([]byte, string, string, error) {
	var data []byte
	var contentType string
	var responseFormat string

	query := `
		SELECT resized_data, content_type, response_format
		FROM image_cache
		WHERE url = ? AND width = ?
		LIMIT 1
	`

	if width == 0 {
		query = `
			SELECT original_data, content_type, response_format
			FROM image_cache
			WHERE url = ? AND width = 0
			LIMIT 1
		`
	}

	err := DB.QueryRow(query, url, width).Scan(&data, &contentType, &responseFormat)
	if err == sql.ErrNoRows {
		return nil, "", "", nil
	}
	if err != nil {
		return nil, "", "", err
	}

	return data, contentType, responseFormat, nil
}

func CacheImage(url string, width int, originalData, resizedData []byte, contentType, responseFormat string) error {
	query := `
		INSERT OR REPLACE INTO image_cache (url, width, original_data, resized_data, content_type, response_format)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := DB.Exec(query, url, width, originalData, resizedData, contentType, responseFormat)
	if err != nil {
		return fmt.Errorf("failed to cache image: %w", err)
	}

	return nil
}

func CacheOriginalImage(url string, data []byte, contentType, responseFormat string) error {
	query := `
		INSERT OR REPLACE INTO image_cache (url, width, original_data, content_type, response_format)
		VALUES (?, 0, ?, ?, ?)
	`

	_, err := DB.Exec(query, url, data, contentType, responseFormat)
	if err != nil {
		return fmt.Errorf("failed to cache original image: %w", err)
	}

	return nil
}

// GetDatabaseSize returns the size of the database file in bytes
func GetDatabaseSize() (int64, error) {
	dbPath := filepath.Join("db", "image_cache.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get database file info: %w", err)
	}
	return info.Size(), nil
}

// DeleteOldestImages deletes the oldest half of cached images
func DeleteOldestImages() error {
	// First, get the total count of images
	var totalCount int
	err := DB.QueryRow("SELECT COUNT(*) FROM image_cache").Scan(&totalCount)
	if err != nil {
		return fmt.Errorf("failed to count images: %w", err)
	}

	if totalCount == 0 {
		return nil // No images to delete
	}

	// Calculate how many to delete (half)
	deleteCount := totalCount / 2
	if deleteCount == 0 {
		deleteCount = 1 // Delete at least one if there are any
	}

	// Delete the oldest images
	query := `
		DELETE FROM image_cache
		WHERE id IN (
			SELECT id FROM image_cache
			ORDER BY created_at ASC
			LIMIT ?
		)
	`

	result, err := DB.Exec(query, deleteCount)
	if err != nil {
		return fmt.Errorf("failed to delete old images: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	log.Printf("Database cleanup: deleted %d old cached images", rowsAffected)
	return nil
}

// StartCleanupService starts a background goroutine that monitors database size
// and cleans up old cached images when the database exceeds 100MB
func StartCleanupService() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		log.Println("Database cleanup service started (checking every minute)")

		for range ticker.C {
			size, err := GetDatabaseSize()
			if err != nil {
				log.Printf("Error getting database size: %v", err)
				continue
			}

			// Convert to MB for easier reading
			sizeMB := float64(size) / (1024 * 1024)

			// Check if database is larger than the limit
			if sizeMB > float64(MaxDatabaseSizeMB) {
				log.Printf("Database size (%.2f MB) exceeds %dMB limit, starting cleanup", sizeMB, MaxDatabaseSizeMB)

				if err := DeleteOldestImages(); err != nil {
					log.Printf("Error during database cleanup: %v", err)
				} else {
					// Get new size after cleanup
					newSize, err := GetDatabaseSize()
					if err == nil {
						newSizeMB := float64(newSize) / (1024 * 1024)
						log.Printf("Database cleanup completed. Size reduced from %.2f MB to %.2f MB", sizeMB, newSizeMB)
					}
				}
			}
		}
	}()
}
