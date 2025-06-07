package main

import (
	"fmt"
	"os"
)

func main() {
	currentDir, _ := os.Getwd()

	config := fmt.Sprintf(`server {
    listen 80;
    server_name resizer.example.com;

    root %s;
    passenger_enabled on;
    passenger_app_type generic;
    passenger_startup_file bin/image_resize;

    # Static file serving
    location /static/ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }

    # Health check
    location /health {
        access_log off;
        return 200 "healthy\n";
    }

    # Client body size for image uploads
    client_max_body_size 50m;

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_types
        text/plain
        text/css
        text/xml
        text/javascript
        application/javascript
        application/xml+rss
        application/json
        image/svg+xml;
}`, currentDir)

	fmt.Print(config)
}
