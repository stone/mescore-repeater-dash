HOST ?= neipa.wg

.PHONY: test build build-neipa deploy demo
test:
	go test ./... && go vet ./...
build:
	go build -o meshtui ./cmd/meshtui
build-neipa:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o meshtui ./cmd/meshtui
deploy: build-neipa
	scp meshtui $(HOST):~/meshtui.new && ssh $(HOST) mv -f meshtui.new meshtui
demo: build
	./meshtui -demo
