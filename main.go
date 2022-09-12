package main

import (
	"os"
	"fmt"
)

func main() {
	conf, err := getConfig(os.Args[1])
	if err != nil {
		fmt.Println("Error: ", err)
		return;
	}
	fmt.Println(conf);
	err = run(conf);
	if err != nil {
		fmt.Println("Error: ", err)
		return;
	}
}
