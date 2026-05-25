CONFIG ?= configs/acorn.local.yaml
BINARY ?= ./bin/acorn
VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
RELEASE_GOOS ?= linux
RELEASE_GOARCH ?= amd64
FAISS_ARTIFACT_DIR ?=
DEV_FAISS_ARTIFACT_DIR ?= .artifacts/faiss-native
DEV_BINARY ?= ./bin/acorn-dev
DIST_DIR ?= ./dist
GO_PACKAGES := $(shell go list ./...)

GOLANGCI_LINT := $(shell which golangci-lint 2>/dev/null)
GOIMPORTS := $(shell which goimports 2>/dev/null)

.PHONY: help build release release-linux-amd64 release-linux-arm64 dev-faiss-artifacts dev-build-faiss dev-doctor-faiss dev-serve-faiss doctor serve test vet lint format format-check

help:
	@echo "make build         # build ./bin/acorn"
	@echo "make release       # build release tarball in ./dist"
	@echo "make release-linux-amd64 # build linux/amd64 release tarball"
	@echo "make release-linux-arm64 # build linux/arm64 release tarball"
	@echo "make dev-faiss-artifacts # build local FAISS artifacts for current darwin/arm64 dev host"
	@echo "make dev-build-faiss # build ./bin/acorn-dev with bleve_faiss vectors tags"
	@echo "make dev-doctor-faiss # run doctor with local FAISS-enabled dev binary"
	@echo "make dev-serve-faiss # run serve with local FAISS-enabled dev binary"
	@echo "make doctor        # run doctor with $(CONFIG)"
	@echo "make serve         # run remote API with $(CONFIG)"
	@echo "make test          # go test ./..."
	@echo "make vet           # go vet ./..."
	@echo "make lint          # run golangci-lint"
	@echo "make format        # auto-fix: goimports"

build:
	@mkdir -p ./bin
	go build -o $(BINARY) ./cmd/acorn

release:
	VERSION="$(VERSION)" RELEASE_GOOS="$(RELEASE_GOOS)" RELEASE_GOARCH="$(RELEASE_GOARCH)" FAISS_ARTIFACT_DIR="$(FAISS_ARTIFACT_DIR)" DIST_DIR="$(DIST_DIR)" sh scripts/build-release.sh

release-linux-amd64:
	$(MAKE) release RELEASE_GOOS=linux RELEASE_GOARCH=amd64

release-linux-arm64:
	$(MAKE) release RELEASE_GOOS=linux RELEASE_GOARCH=arm64

dev-faiss-artifacts:
	rm -rf "$(DEV_FAISS_ARTIFACT_DIR)"
	sh scripts/build-faiss-artifacts.sh "$(DEV_FAISS_ARTIFACT_DIR)" "$$(go env GOOS)" "$$(go env GOARCH)"

dev-build-faiss:
	@mkdir -p ./bin
	scripts/run-with-faiss-artifacts.sh "$(DEV_FAISS_ARTIFACT_DIR)" go build -tags "bleve_faiss vectors" -o "$(DEV_BINARY)" ./cmd/acorn

dev-doctor-faiss: dev-build-faiss
	scripts/run-with-faiss-artifacts.sh "$(DEV_FAISS_ARTIFACT_DIR)" "$(DEV_BINARY)" doctor -c "$(CONFIG)"

dev-serve-faiss: dev-build-faiss
	scripts/run-with-faiss-artifacts.sh "$(DEV_FAISS_ARTIFACT_DIR)" "$(DEV_BINARY)" serve -c "$(CONFIG)"

doctor: build
	$(BINARY) doctor -c $(CONFIG)

serve: build
	$(BINARY) serve -c $(CONFIG)

test:
	go test $(GO_PACKAGES)

test-architecture:
	go test ./tests/architecture

vet:
	go vet $(GO_PACKAGES)

lint:
ifndef GOLANGCI_LINT
	$(error golangci-lint not found — install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
endif
	golangci-lint run ./...

format:
ifndef GOIMPORTS
	$(error goimports not found — install: go install golang.org/x/tools/cmd/goimports@latest)
endif
	gofmt -w ./cmd ./internal
	goimports -w ./cmd ./internal

format-check:
	@test -z "$$(gofmt -l ./cmd ./internal)" || (gofmt -l ./cmd ./internal && false)
ifndef GOIMPORTS
	$(error goimports not found — install: go install golang.org/x/tools/cmd/goimports@latest)
endif
	@test -z "$$(goimports -l ./cmd ./internal)" || (goimports -l ./cmd ./internal && false)
