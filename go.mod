module github.com/signalroute/go-sms-gate

go 1.22

require (
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.1
	github.com/prometheus/client_golang v1.19.1
	go.bug.st/serial v1.6.2
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.29.6
)

replace (
	go.bug.st/serial => /tmp/go-serial
)
