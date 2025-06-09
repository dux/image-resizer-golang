package database

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var RefererDB *sql.DB

func InitRefererDB() error {
	// Create tmp directory if it doesn't exist
	dbDir := "tmp"
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	// Set database path
	dbPath := filepath.Join(dbDir, "http_refers.db")

	var err error
	// Open with optimized settings for performance and concurrency
	RefererDB, err = sql.Open("sqlite3", dbPath+"?_journal=WAL&_busy_timeout=5000&_synchronous=NORMAL&cache=shared")
	if err != nil {
		return fmt.Errorf("failed to open referer database: %w", err)
	}

	// Configure connection pool for better concurrency
	RefererDB.SetMaxOpenConns(1) // SQLite performs best with single writer
	RefererDB.SetMaxIdleConns(1)
	RefererDB.SetConnMaxLifetime(0) // Keep connection alive

	// Enable WAL mode and optimize SQLite settings
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -32000", // 32MB cache
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 134217728", // 128MB mmap
		"PRAGMA busy_timeout = 5000",
	}

	for _, pragma := range pragmas {
		if _, err := RefererDB.Exec(pragma); err != nil {
			return fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	if err := createRefererTables(); err != nil {
		return fmt.Errorf("failed to create referer tables: %w", err)
	}

	log.Printf("Referer database initialized at: %s with WAL mode and optimizations", dbPath)
	return nil
}

func createRefererTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS referer_tracking (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		base_domain TEXT NOT NULL,
		date_requested DATE NOT NULL,
		request_count INTEGER DEFAULT 1,
		is_disabled BOOLEAN DEFAULT FALSE,
		UNIQUE(base_domain, date_requested)
	);

	CREATE INDEX IF NOT EXISTS idx_domain_date ON referer_tracking(base_domain, date_requested);
	`

	_, err := RefererDB.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create referer tables: %w", err)
	}

	log.Println("Referer database tables created successfully")
	return nil
}

// ExtractBaseDomain extracts the base domain from a URL or referer string
func ExtractBaseDomain(referer string) string {
	if referer == "" {
		return "direct"
	}

	// Parse the URL
	u, err := url.Parse(referer)
	if err != nil {
		// If parsing fails, try to extract domain from the string
		parts := strings.Split(referer, "/")
		if len(parts) > 2 {
			return strings.TrimPrefix(parts[2], "www.")
		}
		return "hidden"
	}

	// Get the host and remove www prefix if present
	host := u.Host
	if host == "" {
		host = u.Hostname()
	}
	if host == "" {
		return "hidden"
	}

	// Remove www. prefix
	host = strings.TrimPrefix(host, "www.")
	
	// Remove port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	return host
}

// TrackReferer tracks a referer for resize requests
func TrackReferer(referer string) error {
	baseDomain := ExtractBaseDomain(referer)
	today := time.Now().Format("2006-01-02")

	// Use UPSERT with retry logic
	query := `
		INSERT INTO referer_tracking (base_domain, date_requested, request_count)
		VALUES (?, ?, 1)
		ON CONFLICT(base_domain, date_requested)
		DO UPDATE SET request_count = request_count + 1
	`
	
	// Retry logic for busy database
	var err error
	for i := 0; i < 3; i++ {
		_, err = RefererDB.Exec(query, baseDomain, today)
		if err == nil {
			return nil
		}
		if i < 2 {
			time.Sleep(time.Millisecond * 50)
		}
	}

	return fmt.Errorf("failed to track referer after retries: %w", err)
}

// GetRefererStats returns referer statistics for a given date range
func GetRefererStats(startDate, endDate string) ([]RefererStat, error) {
	query := `
		SELECT base_domain, date_requested, request_count
		FROM referer_tracking
		WHERE date_requested BETWEEN ? AND ?
		ORDER BY date_requested DESC, request_count DESC
	`

	rows, err := RefererDB.Query(query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query referer stats: %w", err)
	}
	defer rows.Close()

	var stats []RefererStat
	for rows.Next() {
		var stat RefererStat
		if err := rows.Scan(&stat.BaseDomain, &stat.DateRequested, &stat.RequestCount); err != nil {
			return nil, fmt.Errorf("failed to scan referer stat: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating referer stats: %w", err)
	}

	return stats, nil
}

// RefererStat represents a referer statistic entry
type RefererStat struct {
	BaseDomain    string
	DateRequested string
	RequestCount  int
}

// DomainStat represents aggregated stats for a domain
type DomainStat struct {
	BaseDomain   string
	TotalCount   int
	IsDisabled   bool
}

// GetAggregatedRefererStats returns referer statistics grouped by domain
func GetAggregatedRefererStats() ([]DomainStat, error) {
	query := `
		SELECT base_domain, SUM(request_count) as total_count, MAX(is_disabled) as is_disabled
		FROM referer_tracking
		GROUP BY base_domain
		ORDER BY total_count DESC
	`

	rows, err := RefererDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregated referer stats: %w", err)
	}
	defer rows.Close()

	var stats []DomainStat
	for rows.Next() {
		var stat DomainStat
		if err := rows.Scan(&stat.BaseDomain, &stat.TotalCount, &stat.IsDisabled); err != nil {
			return nil, fmt.Errorf("failed to scan domain stat: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating domain stats: %w", err)
	}

	return stats, nil
}

// IsDomainDisabled checks if a domain is disabled
func IsDomainDisabled(domain string) (bool, error) {
	query := `
		SELECT COUNT(*) FROM referer_tracking 
		WHERE base_domain = ? AND is_disabled = TRUE
	`
	
	var count int
	err := RefererDB.QueryRow(query, domain).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check domain status: %w", err)
	}
	
	return count > 0, nil
}

// ToggleDomainStatus toggles the disabled status for a domain
func ToggleDomainStatus(domain string) error {
	// Update all records for this domain
	query := `
		UPDATE referer_tracking 
		SET is_disabled = NOT is_disabled 
		WHERE base_domain = ?
	`
	
	_, err := RefererDB.Exec(query, domain)
	if err != nil {
		return fmt.Errorf("failed to toggle domain status: %w", err)
	}
	
	return nil
}