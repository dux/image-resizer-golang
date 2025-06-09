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

  // Retry logic for busy database
  var err error
  for i := 0; i < 3; i++ {
    err = DB.QueryRow(query, url, width).Scan(&data, &contentType, &responseFormat)
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

func CacheImage(url string, width int, originalData, resizedData []byte, contentType, responseFormat string) error {
  query := `
    INSERT OR REPLACE INTO image_cache (url, width, original_data, resized_data, content_type, response_format)
    VALUES (?, ?, ?, ?, ?, ?)
  `

  // Retry logic for busy database
  var err error
  for i := 0; i < 3; i++ {
    _, err = DB.Exec(query, url, width, originalData, resizedData, contentType, responseFormat)
    if err == nil {
      return nil
    }
    if i < 2 {
      time.Sleep(time.Millisecond * 50)
    }
  }

  return fmt.Errorf("failed to cache image after retries: %w", err)
}

func CacheOriginalImage(url string, data []byte, contentType, responseFormat string) error {
  query := `
    INSERT OR REPLACE INTO image_cache (url, width, original_data, content_type, response_format)
    VALUES (?, 0, ?, ?, ?)
  `

  // Retry logic for busy database
  var err error
  for i := 0; i < 3; i++ {
    _, err = DB.Exec(query, url, data, contentType, responseFormat)
    if err == nil {
      return nil
    }
    if i < 2 {
      time.Sleep(time.Millisecond * 50)
    }
  }

  return fmt.Errorf("failed to cache original image after retries: %w", err)
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
