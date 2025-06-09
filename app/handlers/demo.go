package handlers

import (
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type DemoPageData struct {
	ImageURL   string
	EncodedURL string
}

func DemoHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/layout.html", "templates/demo.html")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	src := r.URL.Query().Get("src")

	data := DemoPageData{}

	if src == "" {
		randomImageURL := getRandomUnsplashImage()
		if randomImageURL == "" {
			// Fallback to a default image if fetching fails
			randomImageURL = "https://images.unsplash.com/photo-1506905925346-21bda4d32df4"
		}
		http.Redirect(w, r, "/demo?src="+url.QueryEscape(randomImageURL), http.StatusFound)
		return
	}

	data.ImageURL = src
	data.EncodedURL = strings.Trim(url.QueryEscape(src), `"'`)

	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, data)
}

func getRandomUnsplashImage() string {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Fetch Unsplash homepage
	resp, err := client.Get("https://unsplash.com")
	if err != nil {
		fmt.Printf("Error fetching Unsplash homepage: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		return ""
	}

	// Convert to string
	html := string(body)

	// Find all image URLs in the HTML
	// Look for patterns like: https://images.unsplash.com/photo-[id]
	imageRegex := regexp.MustCompile(`https://images\.unsplash\.com/photo-[a-zA-Z0-9\-]+`)
	matches := imageRegex.FindAllString(html, -1)

	if len(matches) == 0 {
		fmt.Println("No Unsplash images found in HTML")
		return ""
	}

	// Remove duplicates
	uniqueImages := make(map[string]bool)
	var images []string
	for _, match := range matches {
		if !uniqueImages[match] {
			uniqueImages[match] = true
			images = append(images, match)
		}
	}

	// Select a random image
	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(images))
	selectedImage := images[randomIndex]

	// Clean up the URL - remove any query parameters that might be attached
	if idx := strings.Index(selectedImage, "?"); idx != -1 {
		selectedImage = selectedImage[:idx]
	}

	fmt.Printf("Selected random Unsplash image: %s\n", selectedImage)
	return selectedImage
}
