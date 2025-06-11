package handlers

import (
	"bufio"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"net/url"
	"os"
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
		randomImageURL := getRandomImage()
		http.Redirect(w, r, "/demo?src="+url.QueryEscape(randomImageURL), http.StatusFound)
		return
	}

	// Just use the provided URL - let the browser handle loading errors
	data.ImageURL = src
	data.EncodedURL = strings.Trim(url.QueryEscape(src), `"'`)

	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, data)
}

func checkImageAvailable(url string) bool {
	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil // Allow redirects
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Accept any 2xx status code
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func getRandomImage() string {
	file, err := os.Open("static/random-images.txt")
	if err != nil {
		fmt.Printf("Failed to open random-images.txt: %v\n", err)
		return ""
	}
	defer file.Close()

	var images []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		images = append(images, line)
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading random-images.txt: %v\n", err)
		return ""
	}
	if len(images) == 0 {
		fmt.Printf("No images found in random-images.txt\n")
		return ""
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(images), func(i, j int) {
		images[i], images[j] = images[j], images[i]
	})
	for _, imageURL := range images {
		if checkImageAvailable(imageURL) {
			fmt.Printf("Selected available image: %s\n", imageURL)
			return imageURL
		}
		fmt.Printf("Image not available: %s\n", imageURL)
	}
	return images[0]
}
