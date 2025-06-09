default:
	@cat Makefile

build:
	go build -o bin/server app/main.go

passenger:
	go build -o bin/image_resize app/main.go
	go run lib/generate_nginx.go

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

.PHONY: default build passenger run test test-resize test-image clean
