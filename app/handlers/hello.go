package handlers

import (
  "fmt"
  "net/http"
)

func HelloHandler(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Content-Type", "text/html; charset=utf-8")
  fmt.Fprintf(w, "<h1>Hello World!</h1><p>Welcome to the Image Resize Service</p>")
}