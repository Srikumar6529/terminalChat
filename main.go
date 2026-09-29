package main

import (
	"fmt"
	"os"
	"bufio"
)

func main(){
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("You >")
	scanner.Scan()
	prompt := scanner.Text()

	fmt.Println("You Entered: ", prompt)
}
	
