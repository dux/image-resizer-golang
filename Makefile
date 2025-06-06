build:
	go build -o bin/server cmd/server/main.go

run:
	go run cmd/server/main.go

dev:
	find . | entr -r make run

test:
	go test ./...

test-image:
	@curl -s "http://localhost:8080/image/info?path=static/hobo.jpeg" | jq .

clean:
	rm -rf bin/

.PHONY: build run test test-image clean
