# Project variables.
PROJECT_NAME = gnopls
BUILD_FLAGS = -mod=readonly -ldflags='$(LD_FLAGS)'
BUILD_FOLDER = ./build

.PHONY: install build clean

## install: Install the binary.
install:
	@echo Installing $(PROJECT_NAME)...
	@go install $(BUILD_FLAGS) ./...
	@gnopls version

## build: Build the binary.
build:
	@echo Building $(PROJECT_NAME)...
	@-mkdir -p $(BUILD_FOLDER) 2> /dev/null
	@go build $(BUILD_FLAGS) -o $(BUILD_FOLDER) ./...

## clean: Remove build dir. Also runs `go clean`.
clean:
	@echo Cleaning build cache...
	@-rm -rf $(BUILD_FOLDER) 2> /dev/null
	@go clean ./...
