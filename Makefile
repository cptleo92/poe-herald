host_ip = '138.197.67.190'

.PHONY: cleanup
cleanup:
	go mod tidy
	go mod verify
	go fmt ./...
	go vet ./...


.PHONY: prod/connect
prod/connect:
	ssh envoy@$(host_ip)