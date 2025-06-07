package handlers

import (
  "net/http"
)

func FaviconHandler(w http.ResponseWriter, r *http.Request) {
  // Set cache headers for 1 hour
  w.Header().Set("Cache-Control", "public, max-age=3600")
  w.Header().Set("Content-Type", "image/svg+xml")
  
  // Simple camera/image resize icon SVG
  svg := `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">
    <rect width="32" height="32" fill="#2563eb" rx="6"/>
    <path d="M8 10h3l2-2h6l2 2h3c1.1 0 2 .9 2 2v10c0 1.1-.9 2-2 2H8c-1.1 0-2-.9-2-2V12c0-1.1.9-2 2-2z" fill="white"/>
    <circle cx="16" cy="17" r="4" fill="#2563eb"/>
    <circle cx="16" cy="17" r="2" fill="white"/>
    <rect x="20" y="13" width="2" height="1" fill="#2563eb"/>
  </svg>`
  
  w.Write([]byte(svg))
}