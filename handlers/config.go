package handlers

import (
  "encoding/json"
  "fmt"
  "net/http"
  "os"

  "image-resize/database"
)

type ConfigInfo struct {
  Port           string  `json:"port"`
  MaxDBSizeMB    int     `json:"max_db_size_mb"`
  WebPQuality    float32 `json:"webp_quality"`
  DBSizeMB       float64 `json:"db_size_mb"`
  DBSizeBytes    int64   `json:"db_size_bytes"`
  ImageCount     int     `json:"image_count"`
  DBSizeReadable string  `json:"db_size_readable"`
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

  config := ConfigInfo{
    Port:           port,
    MaxDBSizeMB:    database.MaxDatabaseSizeMB,
    WebPQuality:    WebPQuality,
    DBSizeMB:       dbSizeMB,
    DBSizeBytes:    dbSize,
    ImageCount:     imageCount,
    DBSizeReadable: dbSizeReadable,
  }

  // Check if client wants JSON
  if r.Header.Get("Accept") == "application/json" || r.URL.Query().Get("format") == "json" {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(config)
    return
  }

  // Return HTML response
  w.Header().Set("Content-Type", "text/html; charset=utf-8")
  fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Image Resize Service - Configuration</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            max-width: 800px;
            margin: 50px auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            background: white;
            border-radius: 8px;
            padding: 30px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            margin-bottom: 30px;
        }
        .config-section {
            margin-bottom: 30px;
        }
        .config-section h2 {
            color: #555;
            font-size: 1.2em;
            margin-bottom: 15px;
            border-bottom: 2px solid #eee;
            padding-bottom: 10px;
        }
        .config-item {
            display: flex;
            justify-content: space-between;
            padding: 10px 0;
            border-bottom: 1px solid #f0f0f0;
        }
        .config-item:last-child {
            border-bottom: none;
        }
        .label {
            font-weight: 500;
            color: #666;
        }
        .value {
            font-family: 'Courier New', monospace;
            color: #333;
            font-weight: 600;
        }
        .warning {
            color: #ff6b6b;
        }
        .info {
            color: #4dabf7;
        }
        .progress-bar {
            width: 100%%;
            height: 20px;
            background-color: #e9ecef;
            border-radius: 10px;
            overflow: hidden;
            margin-top: 10px;
        }
        .progress-fill {
            height: 100%%;
            background-color: #51cf66;
            transition: width 0.3s ease;
        }
        .progress-fill.warning {
            background-color: #ffd43b;
        }
        .progress-fill.danger {
            background-color: #ff6b6b;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Image Resize Service Configuration</h1>
        
        <div class="config-section">
            <h2>Server Configuration</h2>
            <div class="config-item">
                <span class="label">Port:</span>
                <span class="value">%s</span>
            </div>
            <div class="config-item">
                <span class="label">WebP Quality:</span>
                <span class="value">%.0f</span>
            </div>
        </div>

        <div class="config-section">
            <h2>Database Configuration</h2>
            <div class="config-item">
                <span class="label">Max Database Size:</span>
                <span class="value">%d MB</span>
            </div>
        </div>

        <div class="config-section">
            <h2>Database Statistics</h2>
            <div class="config-item">
                <span class="label">Current Size:</span>
                <span class="value %s">%s</span>
            </div>
            <div class="config-item">
                <span class="label">Image Count:</span>
                <span class="value">%d</span>
            </div>
            <div class="config-item">
                <span class="label">Average Size per Image:</span>
                <span class="value">%s</span>
            </div>
            
            <div style="margin-top: 20px;">
                <div class="label">Database Usage:</div>
                <div class="progress-bar">
                    <div class="progress-fill %s" style="width: %.1f%%;"></div>
                </div>
                <div style="text-align: center; margin-top: 5px; color: #666;">
                    %.1f%% of %d MB limit
                </div>
            </div>
        </div>

        <div style="margin-top: 40px; padding-top: 20px; border-top: 1px solid #eee; color: #999; font-size: 0.9em;">
            <p>Environment Variables:</p>
            <ul style="font-family: 'Courier New', monospace; font-size: 0.9em;">
                <li>PORT=%s</li>
                <li>MAX_DB_SIZE=%d</li>
                <li>QUALITY=%.0f</li>
            </ul>
            <p style="margin-top: 15px;">
                <a href="/config?format=json" style="color: #4dabf7;">View as JSON</a>
            </p>
        </div>
    </div>
</body>
</html>`,
    config.Port,
    config.WebPQuality,
    config.MaxDBSizeMB,
    getSizeClass(dbSizeMB, float64(config.MaxDBSizeMB)),
    config.DBSizeReadable,
    config.ImageCount,
    getAverageImageSize(dbSize, imageCount),
    getProgressClass(dbSizeMB, float64(config.MaxDBSizeMB)),
    getUsagePercent(dbSizeMB, float64(config.MaxDBSizeMB)),
    getUsagePercent(dbSizeMB, float64(config.MaxDBSizeMB)),
    config.MaxDBSizeMB,
    config.Port,
    config.MaxDBSizeMB,
    config.WebPQuality,
  )
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
  return (current / max) * 100
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