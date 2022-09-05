package main

import (
	"os"
	"fmt"
)

func main() {
	conf, err := build(os.Args[1])
	if err != nil {
		fmt.Println("Error: ", err)
		return;
	}
	fmt.Println(conf);
}
