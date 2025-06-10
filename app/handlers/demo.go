package handlers

import (
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"net/url"
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
		randomImageURL := getRandomPixabayImage()
		if randomImageURL == "" {
			// Fallback to a default image if fetching fails
			randomImageURL = "https://cdn.pixabay.com/photo/2015/04/23/22/00/tree-736885_640.jpg"
		}
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

func getRandomPixabayImage() string {
	// Use a pool of pre-selected high-quality Pixabay images
	// These are free to use under Pixabay License
	pixabayImages := []string{
		"https://cdn.pixabay.com/photo/2015/04/23/22/00/tree-736885_640.jpg",
		"https://cdn.pixabay.com/photo/2017/02/08/17/24/fantasy-2049567_640.jpg",
		"https://cdn.pixabay.com/photo/2018/01/14/23/12/nature-3082832_640.jpg",
		"https://cdn.pixabay.com/photo/2016/05/05/02/37/sunset-1373171_640.jpg",
		"https://cdn.pixabay.com/photo/2017/12/15/13/51/polynesia-3021072_640.jpg",
		"https://cdn.pixabay.com/photo/2018/08/14/13/23/ocean-3605547_640.jpg",
		"https://cdn.pixabay.com/photo/2016/11/29/05/45/astronomy-1867616_640.jpg",
		"https://cdn.pixabay.com/photo/2017/01/16/15/17/hot-air-balloons-1984308_640.jpg",
		"https://cdn.pixabay.com/photo/2016/11/18/16/19/flowers-1835619_640.jpg",
		"https://cdn.pixabay.com/photo/2017/08/30/01/05/milky-way-2695569_640.jpg",
		"https://cdn.pixabay.com/photo/2018/04/27/03/50/portrait-3353699_640.jpg",
		"https://cdn.pixabay.com/photo/2017/05/09/03/46/alberta-2297204_640.jpg",
		"https://cdn.pixabay.com/photo/2016/10/22/20/58/mountain-1761292_640.jpg",
		"https://cdn.pixabay.com/photo/2018/05/28/22/11/lake-3435756_640.jpg",
		"https://cdn.pixabay.com/photo/2017/04/05/01/16/lilac-2204483_640.jpg",
	}
	
	// Shuffle the array to get random order
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(pixabayImages), func(i, j int) {
		pixabayImages[i], pixabayImages[j] = pixabayImages[j], pixabayImages[i]
	})
	
	// Try each image until we find one that works
	for _, imageURL := range pixabayImages {
		if checkImageAvailable(imageURL) {
			fmt.Printf("Selected available Pixabay image: %s\n", imageURL)
			return imageURL
		}
		fmt.Printf("Image not available (403 or other error): %s\n", imageURL)
	}
	
	// If all fail, return the first one anyway
	fmt.Printf("All images failed, using first as fallback: %s\n", pixabayImages[0])
	return pixabayImages[0]
}
