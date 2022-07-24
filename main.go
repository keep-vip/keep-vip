package main

import "keep-vip/cmd"

// GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o keep-vip main.go
func main() {
	cmd.Execute()
}
