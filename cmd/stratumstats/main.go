package main

import (
	"os"

	"github.com/proofofmike/stratumstats/internal/app"
)

func main() {
	app.Main(os.Args[1:])
}
