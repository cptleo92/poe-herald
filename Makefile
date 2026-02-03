host_ip = '138.197.67.190'

.PHONY: prod/connect
prod/connect:
	ssh envoy@$(host_ip)