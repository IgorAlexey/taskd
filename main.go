package main

import (
	"os"

	"github.com/IgorAlexey/taskd/internal/taskd"
)

func main() {
	os.Exit(taskd.Main(os.Stdout, os.Stderr, os.Stdin, os.Args[1:]))
}
