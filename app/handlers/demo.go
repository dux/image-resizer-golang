package handlers

import (
	"html/template"
	"net/http"
	"net/url"
)

type DemoPageData struct {
	ImageURL   string
	EncodedURL string
}

func DemoHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/demo.html")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	src := r.URL.Query().Get("src")

	data := DemoPageData{}

	if src == "" {
		randomImageURL := "https:/images.unsplash.com/photo-1749154362898-860b54bdf363"
		http.Redirect(w, r, "/demo?src="+url.QueryEscape(randomImageURL), http.StatusFound)
		return
	}

	data.ImageURL = src
	data.EncodedURL = url.QueryEscape(src)

	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, data)
}
