package handlers

import (
	"encoding/json"
	"net/http"

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

	img, err := models.NewImage(imagePath)
	if err != nil {
		http.Error(w, "Failed to read image: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(img.GetProperties())
}
