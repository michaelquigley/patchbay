.DEFAULT_GOAL := build
GOBIN ?= $(shell go env GOPATH)/bin

ifeq ($(filter-out /,$(abspath $(GOBIN))),)
$(error GOBIN is '$(GOBIN)'; it must name a real directory)
endif

# libpipewire's pkg-config cflags carry -fno-strict-overflow, which cgo refuses unless allowed.
export CGO_CFLAGS_ALLOW := -fno-strict-overflow

.PHONY: build test clean push

build:
	go install ./...

test:
	go test ./... -count=1
	go vet ./...

clean:
	go clean ./...
	rm -f "$(GOBIN)"/*

push: build
	push vendor "$(GOBIN)/patchbay" patchbay
