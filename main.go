package main

import (
	"os"

	"github.com/M45Core/StratumStats/internal/app"
)

func main() {
	app.Main(os.Args[1:])
}
