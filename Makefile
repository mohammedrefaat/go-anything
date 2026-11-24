.PHONY: all test vet fmt demo-node demo-python

all: fmt vet test

fmt:
	gofmt -s -w .

vet:
	go vet ./...

test:
	go test ./...

demo-node:
	@echo "Running External Port #1 (Node.js REST API -> Go) Benchmark..."
	node demo/real-world/node-rest-api/bench.js

demo-python:
	@echo "Running External Port #2 (Python Worker -> Go) Benchmark..."
	python demo/real-world/python-worker/bench.py
