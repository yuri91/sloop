package main

import (
	"fmt"
	"os"

	"yuri91/sloop/cmd"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}
}
