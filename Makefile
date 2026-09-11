BINARY := tyschem
BACKEND_DIR := backend

.PHONY: all build run dev clean tidy

all: build

build:
	cd $(BACKEND_DIR) && go build -o $(BINARY) .

run: build
	cd $(BACKEND_DIR) && ./$(BINARY)

dev:
	cd $(BACKEND_DIR) && go run .

tidy:
	cd $(BACKEND_DIR) && go mod tidy

clean:
	rm -f $(BACKEND_DIR)/$(BINARY)
