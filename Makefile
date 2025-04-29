# Running on Windows
#
# Set GOPATH in terminal. Example (make for windows needs forwardslashes):
#   set GOPATH=T:/repos/reporter

# Define output binary name and location
BINARY_NAME=grafana-reporter
BINARY_PATH=./bin/$(BINARY_NAME)

# For Windows
ifeq ($(OS),Windows_NT)
	BINARY_NAME := $(BINARY_NAME).exe
	BINARY_PATH := ./bin/$(BINARY_NAME)
endif

# Targets
.PHONY: all build buildlinux clean docker-build docker-push test

all: build

build:
	mkdir -p ./bin
	go build -o $(BINARY_PATH) ./cmd/grafana-reporter

buildlinux:
	mkdir -p ./bin
ifeq ($(OS),Windows_NT)
	set GOOS=linux&& go build -o ./bin/$(BINARY_NAME)-linux ./cmd/grafana-reporter
else
	GOOS=linux go build -o ./bin/$(BINARY_NAME)-linux ./cmd/grafana-reporter
endif

clean:
	rm -rf ./bin

docker-build:
	docker build -t izakmarais/grafana-reporter:2.3.0 -t izakmarais/grafana-reporter:latest .

docker-push:
	docker push izakmarais/grafana-reporter

test:
	go test -v ./...
