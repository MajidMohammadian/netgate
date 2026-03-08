BINARY_NAME := netgate
BIN_DIR     := bin
LINUX_BIN   := $(BIN_DIR)/$(BINARY_NAME)

.PHONY: build build-linux clean

build:
	go build -o $(BINARY_NAME) .

ifeq ($(OS),Windows_NT)
build-linux:
	powershell -ExecutionPolicy Bypass -File build-linux.ps1
else
build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(LINUX_BIN) .
	@echo "Linux binary: $(LINUX_BIN)"
endif

clean:
	rm -f $(BINARY_NAME) $(LINUX_BIN)
