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
	RefererDB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open referer database: %w", err)
	}

	if err := createRefererTables(); err != nil {
		return fmt.Errorf("failed to create referer tables: %w", err)
	}

	log.Printf("Referer database initialized at: %s", dbPath)
	return nil
}

func createRefererTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS referer_tracking (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		base_domain TEXT NOT NULL,
		date_requested DATE NOT NULL,
		request_count INTEGER DEFAULT 1,
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
		return "unknown"
	}

	// Get the host and remove www prefix if present
	host := u.Host
	if host == "" {
		host = u.Hostname()
	}
	if host == "" {
		return "unknown"
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

	// Try to increment existing record first
	query := `
		UPDATE referer_tracking 
		SET request_count = request_count + 1 
		WHERE base_domain = ? AND date_requested = ?
	`
	
	result, err := RefererDB.Exec(query, baseDomain, today)
	if err != nil {
		return fmt.Errorf("failed to update referer tracking: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// If no rows were updated, insert a new record
	if rowsAffected == 0 {
		insertQuery := `
			INSERT INTO referer_tracking (base_domain, date_requested, request_count)
			VALUES (?, ?, 1)
		`
		_, err = RefererDB.Exec(insertQuery, baseDomain, today)
		if err != nil {
			return fmt.Errorf("failed to insert referer tracking: %w", err)
		}
	}

	return nil
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