package main

import (
	"flag"
	"fmt"
	"os"
	"github.com/kr/pretty"
)

func main() {
	f := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	check := f.Bool("check", false, "just check the configuration")
	f.Parse(os.Args[1:])
	err := os.Chdir(f.Arg(0))
	if err != nil {
		fmt.Println("Error: ", err)
		return;
	}
	conf, err := getConfig(".")
	if err != nil {
		fmt.Println("Error: ", err)
		return;
	}
	if *check {
		fmt.Printf("%# v\n", pretty.Formatter(conf));
		return;
	}
	err = run(conf);
	if err != nil {
		fmt.Println("Error: ", err)
		return;
	}
}
