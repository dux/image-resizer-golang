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

// DefaultMaxDatabaseSizeMB is the default maximum database size in megabytes
const DefaultMaxDatabaseSizeMB = 1000

// MaxDatabaseSizeMB is the maximum allowed database size in megabytes
var MaxDatabaseSizeMB int

func init() {
	// Read MAX_DB_SIZE from environment or use default
	maxDBSize := os.Getenv("MAX_DB_SIZE")
	if maxDBSize != "" {
		size, err := strconv.Atoi(maxDBSize)
		if err != nil {
			log.Printf("Invalid MAX_DB_SIZE value '%s', using default %dMB", maxDBSize, DefaultMaxDatabaseSizeMB)
			MaxDatabaseSizeMB = DefaultMaxDatabaseSizeMB
		} else {
			MaxDatabaseSizeMB = size
			log.Printf("Database size limit set to %dMB from MAX_DB_SIZE env", MaxDatabaseSizeMB)
		}
	} else {
		MaxDatabaseSizeMB = DefaultMaxDatabaseSizeMB
	}
}

func InitDB() error {
	// Create tmp directory if it doesn't exist
	dbDir := "tmp"
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	// Set database path
	dbPath := filepath.Join(dbDir, "image_cache.db")

	var err error
	// Open with optimized settings for performance and concurrency
	DB, err = sql.Open("sqlite3", dbPath+"?_journal=WAL&_busy_timeout=5000&_synchronous=NORMAL&cache=shared")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for better concurrency
	DB.SetMaxOpenConns(1) // SQLite performs best with single writer
	DB.SetMaxIdleConns(1)
	DB.SetConnMaxLifetime(0) // Keep connection alive

	// Enable WAL mode and optimize SQLite settings
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -64000", // 64MB cache
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 268435456", // 256MB mmap
		"PRAGMA busy_timeout = 5000",
	}

	for _, pragma := range pragmas {
		if _, err := DB.Exec(pragma); err != nil {
			return fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Printf("Database initialized at: %s with WAL mode and optimizations", dbPath)
	return nil
}

func createTables() error {
	// Migrate old schema first (before creating new table schema)
	migrated := migrateOldSchema()

	if !migrated {
		// Only create table if migration didn't already recreate it
		query := `
    CREATE TABLE IF NOT EXISTS image_cache (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      url TEXT NOT NULL,
      cache_key TEXT NOT NULL DEFAULT '',
      resized_data BLOB,
      content_type TEXT,
      response_format TEXT,
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
      UNIQUE(url, cache_key)
    );
    CREATE INDEX IF NOT EXISTS idx_url_cache_key ON image_cache(url, cache_key);
    `
		if _, err := DB.Exec(query); err != nil {
			return fmt.Errorf("failed to create tables: %w", err)
		}
	}

	log.Println("Database initialized successfully")
	return nil
}

// migrateOldSchema handles migration from old width-based schema to cache_key-based schema.
// Returns true if migration was performed (table was recreated).
func migrateOldSchema() bool {
	// Check if old 'width' column exists
	rows, err := DB.Query("PRAGMA table_info(image_cache)")
	if err != nil {
		return false
	}
	defer rows.Close()

	hasWidth := false
	hasCacheKey := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == "width" {
			hasWidth = true
		}
		if name == "cache_key" {
			hasCacheKey = true
		}
	}

	if hasWidth && !hasCacheKey {
		log.Println("Migrating database from old width-based schema to cache_key schema...")
		// Old schema detected - recreate table
		tx, err := DB.Begin()
		if err != nil {
			log.Printf("Migration failed to begin transaction: %v", err)
			return false
		}
		defer tx.Rollback()

		migrations := []string{
			`ALTER TABLE image_cache RENAME TO image_cache_old`,
			`CREATE TABLE image_cache (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        url TEXT NOT NULL,
        cache_key TEXT NOT NULL DEFAULT '',
        resized_data BLOB,
        content_type TEXT,
        response_format TEXT,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(url, cache_key)
      )`,
			`INSERT INTO image_cache (url, cache_key, resized_data, content_type, response_format, created_at)
       SELECT url, 'w_' || CAST(width AS TEXT), COALESCE(resized_data, original_data), content_type, response_format, created_at
       FROM image_cache_old WHERE width > 0`,
			`DROP TABLE image_cache_old`,
			`CREATE INDEX IF NOT EXISTS idx_url_cache_key ON image_cache(url, cache_key)`,
		}

		for _, m := range migrations {
			if _, err := tx.Exec(m); err != nil {
				log.Printf("Migration step failed: %v", err)
				return false
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Migration commit failed: %v", err)
			return false
		}
		log.Println("Database migration completed successfully")
		return true
	}

	return false
}

func GetCachedImage(url string, cacheKey string) ([]byte, string, string, error) {
	var data []byte
	var contentType string
	var responseFormat string

	query := `
    SELECT resized_data, content_type, response_format
    FROM image_cache
    WHERE url = ? AND cache_key = ?
    LIMIT 1
  `

	// Retry logic for busy database
	var err error
	for i := 0; i < 3; i++ {
		err = DB.QueryRow(query, url, cacheKey).Scan(&data, &contentType, &responseFormat)
		if err == nil || err == sql.ErrNoRows {
			break
		}
		if i < 2 {
			time.Sleep(time.Millisecond * 50)
		}
	}

	if err == sql.ErrNoRows {
		return nil, "", "", nil
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("database query failed after retries: %w", err)
	}

	return data, contentType, responseFormat, nil
}

func CacheImage(url string, cacheKey string, resizedData []byte, contentType, responseFormat string) error {
	query := `
    INSERT OR REPLACE INTO image_cache (url, cache_key, resized_data, content_type, response_format)
    VALUES (?, ?, ?, ?, ?)
  `

	// Retry logic for busy database
	var err error
	for i := 0; i < 3; i++ {
		_, err = DB.Exec(query, url, cacheKey, resizedData, contentType, responseFormat)
		if err == nil {
			return nil
		}
		if i < 2 {
			time.Sleep(time.Millisecond * 50)
		}
	}

	return fmt.Errorf("failed to cache image after retries: %w", err)
}

// CachedImageInfo holds metadata about a cached image (without the blob data)
type CachedImageInfo struct {
	ID           int    `json:"id"`
	URL          string `json:"url"`
	CacheKey     string `json:"cache_key"`
	ContentType  string `json:"content_type"`
	Format       string `json:"format"`
	Size         int    `json:"size"`
	SizeReadable string `json:"size_readable"`
	CreatedAt    string `json:"created_at"`
}

// CachedImagePage holds a page of cached images plus pagination info
type CachedImagePage struct {
	Images     []CachedImageInfo
	Page       int
	PerPage    int
	TotalCount int
	TotalPages int
}

// ListCachedImages returns a paginated list of cached image metadata (no blob data)
func ListCachedImages(page, perPage int) (*CachedImagePage, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}

	// Get total count
	var totalCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM image_cache").Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to count cached images: %w", err)
	}

	totalPages := (totalCount + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * perPage
	query := `
		SELECT id, url, cache_key, content_type, COALESCE(response_format, ''), length(resized_data), created_at
		FROM image_cache
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`
	rows, err := DB.Query(query, perPage, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list cached images: %w", err)
	}
	defer rows.Close()

	var images []CachedImageInfo
	for rows.Next() {
		var img CachedImageInfo
		if err := rows.Scan(&img.ID, &img.URL, &img.CacheKey, &img.ContentType, &img.Format, &img.Size, &img.CreatedAt); err != nil {
			continue
		}
		if img.Size >= 1024*1024 {
			img.SizeReadable = fmt.Sprintf("%.1f MB", float64(img.Size)/(1024*1024))
		} else {
			img.SizeReadable = fmt.Sprintf("%.1f KB", float64(img.Size)/1024)
		}
		images = append(images, img)
	}

	return &CachedImagePage{
		Images:     images,
		Page:       page,
		PerPage:    perPage,
		TotalCount: totalCount,
		TotalPages: totalPages,
	}, nil
}

// GetCachedImageByID returns the raw blob data for a cached image by ID
func GetCachedImageByID(id int) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := DB.QueryRow("SELECT resized_data, content_type FROM image_cache WHERE id = ?", id).Scan(&data, &contentType)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

// DeleteCachedImage deletes a single cached image by ID
func DeleteCachedImage(id int) error {
	_, err := DB.Exec("DELETE FROM image_cache WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete cached image: %w", err)
	}
	return nil
}

// GetDatabaseSize returns the size of the database file in bytes
func GetDatabaseSize() (int64, error) {
	dbPath := filepath.Join("tmp", "image_cache.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get database file info: %w", err)
	}
	return info.Size(), nil
}

// DeleteOldestImages deletes the oldest half of cached images
func DeleteOldestImages() error {
	// Use a transaction for atomic cleanup
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// First, get the total count of images
	var totalCount int
	err = tx.QueryRow("SELECT COUNT(*) FROM image_cache").Scan(&totalCount)
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

	result, err := tx.Exec(query, deleteCount)
	if err != nil {
		return fmt.Errorf("failed to delete old images: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit cleanup transaction: %w", err)
	}

	// VACUUM in the background to reclaim space
	go func() {
		if _, err := DB.Exec("VACUUM"); err != nil {
			log.Printf("Failed to vacuum database: %v", err)
		}
	}()

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
