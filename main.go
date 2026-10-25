package main

import (
	"context"
	"embed"
	"log"

	"weatherhds/internal/server"
)

//go:embed public admin
var embeddedWebroot embed.FS

func main() {
	if err := server.Run(context.Background(), embeddedWebroot); err != nil {
		log.Fatal(err)
	}
}
