.PHONY: build run clean dist tidy hashpass

tidy:
	go mod tidy

build:
	go build -o ditchfork .

run: build
	./ditchfork

clean:
	rm -f ditchfork ditchfork-*

# Build binaries for all platforms
dist:
	GOOS=linux   GOARCH=amd64        go build -ldflags="-s -w" -o ditchfork-linux-amd64   .
	GOOS=linux   GOARCH=arm64        go build -ldflags="-s -w" -o ditchfork-linux-arm64   .
	GOOS=linux   GOARCH=arm GOARM=7  go build -ldflags="-s -w" -o ditchfork-linux-armv7   .
	GOOS=darwin  GOARCH=amd64        go build -ldflags="-s -w" -o ditchfork-darwin-amd64  .
	GOOS=darwin  GOARCH=arm64        go build -ldflags="-s -w" -o ditchfork-darwin-arm64  .
	GOOS=windows GOARCH=amd64        go build -ldflags="-s -w" -o ditchfork-windows-amd64.exe .

hashpass:
	go run tools/hashpass.go $(PASS)
