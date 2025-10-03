package main

import (
	"context"
	"log"
	"os"

	"github.com/kijimaD/xruler/internal/cli"
)

func main() {
	cmd := cli.NewCommand()

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
