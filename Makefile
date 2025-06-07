build:
	go build -o bin/server app/main.go

run:
	go run app/main.go

dev:
	find app | entr -r make run

test:
	go test ./...

test-resize:
	go test ./test -v -run TestResize

test-image:
	@curl -s "http://localhost:8080/i?path=static/hobo.jpeg" | jq .

clean:
	rm -rf bin/

.PHONY: build run test test-resize test-image clean
