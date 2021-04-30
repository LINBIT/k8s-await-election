GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GO_FILES := $(shell find . -type f -name '*.go' -or -name "go.*")
VERSION ?= $(shell git describe --dirty --always --tags --match "v*.*")
GO_LDFLAGS := '-extldflags "-static" -w -s -X "main.Version=$(VERSION)"'

out/k8s-await-election-$(GOARCH): $(GO_FILES)
	GOARCH=$(GOARCH) GOOS=linux CGO_ENABLED=0 go build -ldflags $(GO_LDFLAGS) -o $@
