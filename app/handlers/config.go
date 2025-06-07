package handlers

import (
  "encoding/json"
  "fmt"
  "html/template"
  "io"
  "net/http"
  "os"
  "path/filepath"
  "strconv"
  "log"

  "image-resize/app/database"
)

// MaxAge is the cache max-age in seconds
var MaxAge int

func init() {
  // Read MAX_AGE from environment or use default (1 day = 86400 seconds)
  maxAgeStr := os.Getenv("MAX_AGE")
  if maxAgeStr != "" {
    age, err := strconv.Atoi(maxAgeStr)
    if err != nil || age < 0 {
      log.Printf("Invalid MAX_AGE value '%s', must be >= 0, using default 86400 (1 day)", maxAgeStr)
      MaxAge = 86400
    } else {
      MaxAge = age
      log.Printf("Cache max-age set to %d seconds from MAX_AGE env", MaxAge)
    }
  } else {
    MaxAge = 86400 // 1 day default
  }
}

type ConfigInfo struct {
  Port             string                   `json:"port"`
  MaxDBSizeMB      int                      `json:"max_db_size_mb"`
  WebPQuality      int                      `json:"webp_quality"`
  DBSizeMB         float64                  `json:"db_size_mb"`
  DBSizeBytes      int64                    `json:"db_size_bytes"`
  ImageCount       int                      `json:"image_count"`
  DBSizeReadable   string                   `json:"db_size_readable"`
  RefererStats     []database.DomainStat    `json:"referer_stats"`
  // Additional fields for template
  SizeClass        string                   `json:"-"`
  ProgressClass    string                   `json:"-"`
  UsagePercent     float64                  `json:"-"`
  AverageImageSize string                   `json:"-"`
}

func ConfigHandler(w http.ResponseWriter, r *http.Request) {
  // Get database size
  dbSize, err := database.GetDatabaseSize()
  if err != nil {
    http.Error(w, fmt.Sprintf("Failed to get database size: %v", err), http.StatusInternalServerError)
    return
  }

  // Get image count
  var imageCount int
  err = database.DB.QueryRow("SELECT COUNT(*) FROM image_cache").Scan(&imageCount)
  if err != nil {
    http.Error(w, fmt.Sprintf("Failed to count images: %v", err), http.StatusInternalServerError)
    return
  }

  // Get port from environment
  port := os.Getenv("PORT")
  if port == "" {
    port = "8080"
  }

  // Convert database size to MB
  dbSizeMB := float64(dbSize) / (1024 * 1024)

  // Create readable size string
  var dbSizeReadable string
  if dbSizeMB >= 1 {
    dbSizeReadable = fmt.Sprintf("%.2f MB", dbSizeMB)
  } else {
    dbSizeKB := float64(dbSize) / 1024
    dbSizeReadable = fmt.Sprintf("%.2f KB", dbSizeKB)
  }

  // Get referer statistics
  refererStats, err := database.GetAggregatedRefererStats()
  if err != nil {
    // Log error but don't fail the whole page
    fmt.Printf("Failed to get referer stats: %v\n", err)
    refererStats = []database.DomainStat{}
  }

  usagePercent := getUsagePercent(dbSizeMB, float64(database.MaxDatabaseSizeMB))
  
  config := ConfigInfo{
    Port:             port,
    MaxDBSizeMB:      database.MaxDatabaseSizeMB,
    WebPQuality:      WebPQuality,
    DBSizeMB:         dbSizeMB,
    DBSizeBytes:      dbSize,
    ImageCount:       imageCount,
    DBSizeReadable:   dbSizeReadable,
    RefererStats:     refererStats,
    SizeClass:        getSizeClass(dbSizeMB, float64(database.MaxDatabaseSizeMB)),
    ProgressClass:    getProgressClass(dbSizeMB, float64(database.MaxDatabaseSizeMB)),
    UsagePercent:     usagePercent,
    AverageImageSize: getAverageImageSize(dbSize, imageCount),
  }

  // Check if client wants JSON
  if r.Header.Get("Accept") == "application/json" || r.URL.Query().Get("format") == "json" {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(config)
    return
  }

  // Return HTML response using template
  tmplPath := filepath.Join("templates", "config.html")
  tmpl, err := template.ParseFiles(tmplPath)
  if err != nil {
    http.Error(w, fmt.Sprintf("Failed to load template: %v", err), http.StatusInternalServerError)
    return
  }

  w.Header().Set("Content-Type", "text/html; charset=utf-8")
  if err := tmpl.Execute(w, config); err != nil {
    http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
    return
  }
}

func getSizeClass(current, max float64) string {
  percent := (current / max) * 100
  if percent >= 90 {
    return "warning"
  }
  return ""
}

func getProgressClass(current, max float64) string {
  percent := (current / max) * 100
  if percent >= 90 {
    return "danger"
  } else if percent >= 70 {
    return "warning"
  }
  return ""
}

func getUsagePercent(current, max float64) float64 {
  if max == 0 {
    return 0
  }
  percent := (current / max) * 100
  // Round to single digit (nearest integer)
  return float64(int(percent + 0.5))
}

func getAverageImageSize(totalSize int64, imageCount int) string {
  if imageCount == 0 {
    return "N/A"
  }
  avgSize := float64(totalSize) / float64(imageCount)
  if avgSize >= 1024*1024 {
    return fmt.Sprintf("%.2f MB", avgSize/(1024*1024))
  }
  return fmt.Sprintf("%.2f KB", avgSize/1024)
}

type ToggleRequest struct {
  Domain string `json:"domain"`
}

type ToggleResponse struct {
  Success bool   `json:"success"`
  Error   string `json:"error,omitempty"`
}

func ToggleDomainHandler(w http.ResponseWriter, r *http.Request) {
  if r.Method != http.MethodPost {
    http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    return
  }

  body, err := io.ReadAll(r.Body)
  if err != nil {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(ToggleResponse{
      Success: false,
      Error:   "Failed to read request body",
    })
    return
  }

  var req ToggleRequest
  if err := json.Unmarshal(body, &req); err != nil {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(ToggleResponse{
      Success: false,
      Error:   "Invalid JSON",
    })
    return
  }

  if req.Domain == "" {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(ToggleResponse{
      Success: false,
      Error:   "Domain is required",
    })
    return
  }

  // Don't allow toggling special domains
  if req.Domain == "direct" || req.Domain == "hidden" {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(ToggleResponse{
      Success: false,
      Error:   "Cannot toggle status for special domains",
    })
    return
  }

  err = database.ToggleDomainStatus(req.Domain)
  if err != nil {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(ToggleResponse{
      Success: false,
      Error:   fmt.Sprintf("Failed to toggle domain status: %v", err),
    })
    return
  }

  w.Header().Set("Content-Type", "application/json")
  json.NewEncoder(w).Encode(ToggleResponse{
    Success: true,
  })
}