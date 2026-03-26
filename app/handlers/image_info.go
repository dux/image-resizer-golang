package handlers

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"image-resize/app/models"
)

func ImageInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	imagePath := r.URL.Query().Get("src")
	if imagePath == "" {
		http.Error(w, "Missing image path parameter", http.StatusBadRequest)
		return
	}

	// Prevent path traversal - only allow files from the static directory
	cleanPath := filepath.Clean(imagePath)
	if strings.Contains(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		http.Error(w, "Invalid image path", http.StatusBadRequest)
		return
	}
	// Restrict to static/ directory only
	safePath := filepath.Join("static", cleanPath)

	img, err := models.NewImage(safePath)
	if err != nil {
		http.Error(w, "Failed to read image: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(img.GetProperties())
}
