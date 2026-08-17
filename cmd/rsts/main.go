package main

import (
	"os"

	"rsts"
)

func main() {
	os.Exit(rsts.Main(os.Args[1:]))
}
