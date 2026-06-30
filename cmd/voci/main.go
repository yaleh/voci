package main

import (
	"os"

	"github.com/yaleh/voci/internal/wire"
)

func main() {
	os.Exit(wire.Run(os.Args))
}
