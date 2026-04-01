package handlers

import (
	"html/template"
	"log"
	"net/http"
)

// Template functions for arithmetic in templates
var templateFuncs = template.FuncMap{
	"add":      func(a, b int) int { return a + b },
	"subtract": func(a, b int) int { return a - b },
}

// Pre-parsed templates (initialized once at startup)
var (
	homeTemplate   *template.Template
	demoTemplate   *template.Template
	configTemplate *template.Template
	logsTemplate   *template.Template
	cacheTemplate  *template.Template
)

func init() {
	var err error

	homeTemplate, err = template.ParseFiles("templates/layout.html", "templates/home.html")
	if err != nil {
		log.Printf("Warning: failed to parse home template: %v", err)
	}

	demoTemplate, err = template.ParseFiles("templates/layout.html", "templates/demo.html")
	if err != nil {
		log.Printf("Warning: failed to parse demo template: %v", err)
	}

	configTemplate, err = template.ParseFiles("templates/layout.html", "templates/config.html")
	if err != nil {
		log.Printf("Warning: failed to parse config template: %v", err)
	}

	logsTemplate, err = template.ParseFiles("templates/layout.html", "templates/logs.html")
	if err != nil {
		log.Printf("Warning: failed to parse logs template: %v", err)
	}

	cacheTemplate, err = template.New("layout.html").Funcs(templateFuncs).ParseFiles("templates/layout.html", "templates/cache.html")
	if err != nil {
		log.Printf("Warning: failed to parse cache template: %v", err)
	}
}

func HomeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve the home page for the exact root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if homeTemplate == nil {
		http.Error(w, "Template not available", http.StatusInternalServerError)
		return
	}

	data := struct {
		CurrentPath string
	}{
		CurrentPath: r.URL.Path,
	}

	err := homeTemplate.Execute(w, data)
	if err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}
