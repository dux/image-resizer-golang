default:
	@cat Makefile

nginx:
	@echo 'server {'
	@echo '  listen 80;'
	@echo '  server_name resizer.foobar.com;'
	@echo ''
	@echo '  location / {'
	@echo '    proxy_pass         http://127.0.0.1:4000;'
	@echo '    proxy_http_version 1.1;'
	@echo '    proxy_set_header   Host              $$host;'
	@echo '    proxy_set_header   X-Real-IP         $$remote_addr;'
	@echo '    proxy_set_header   X-Forwarded-For   $$proxy_add_x_forwarded_for;'
	@echo '    proxy_set_header   X-Forwarded-Proto $$scheme;'
	@echo ''
	@echo '    # WebSocket support'
	@echo '    proxy_set_header   Upgrade           $$http_upgrade;'
	@echo '    proxy_set_header   Connection        "upgrade";'
	@echo '  }'
	@echo '}'

systemd:
	@echo '[Unit]'
	@echo 'Description=Image Resize Server'
	@echo 'After=network.target'
	@echo ''
	@echo '[Service]'
	@echo 'Type=simple'
	@echo 'User=www-data'
	@echo 'WorkingDirectory=$(PWD)'
	@echo 'ExecStart=$(PWD)/bin/server'
	@echo 'Environment="PORT=4000"'
	@echo 'Restart=always'
	@echo 'RestartSec=5'
	@echo ''
	@echo '[Install]'
	@echo 'WantedBy=multi-user.target'

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

re-deploy:
	git stash
	git pull
	make build
	sysd restart

.PHONY: default build passenger run test test-resize test-image clean nginx systemd
