host_ip = '138.197.67.190'

.PHONY: build
build:
	@echo 'Building...'
	go build -ldflags='-s' -o=./bin/bot ./cmd
	GOOS=linux GOARCH=amd64 go build -ldflags='-s' -o=./bin/linux_amd64/bot ./cmd

.PHONY: cleanup
cleanup:
	go mod tidy
	go mod verify
	go fmt ./...
	go vet ./...

.PHONY: prod/connect
prod/connect:
	ssh herald@$(host_ip)

.PHONY: prod/db
prod/db:
	@echo "Connecting to production database..."
	ssh -t herald@$(host_ip) 'set -a && source ~/poe-herald.env && set +a && psql $$DB_DSN'

.PHONY: prod/deploy
prod/deploy: build
	rsync -P ./bin/linux_amd64/bot herald@$(host_ip):~
	rsync -rP --delete ./migrations herald@$(host_ip):~
	ssh -t herald@$(host_ip) 'migrate -path ~/migrations -database $$DB_DSN up'
	rsync -P ./remote/bot.service herald@$(host_ip):~
	rsync -P ./remote/Caddyfile herald@$(host_ip):~
	@test -f ./remote/.env || (echo "Create remote/env with production vars" && exit 1)
	rsync -P ./remote/.env herald@$(host_ip):~/poe-herald.env
	ssh -t herald@$(host_ip) 'sudo mv ~/bot.service /etc/systemd/system/ && sudo systemctl enable bot && sudo systemctl restart bot && sudo mv ~/Caddyfile /etc/caddy/ && sudo systemctl reload caddy'

.PHONY: prod/down
prod/down:
	ssh -t herald@$(host_ip) 'sudo systemctl stop bot && sudo systemctl disable bot'