package main

import (
	"fmt"
	"os"
	"bufio"
)
func getInput() string {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("You > ")
	scanner.Scan()
	prompt := scanner.Text()
	return prompt
}
func main(){
	prompt := getInput()	
	fmt.Println("You Entered: ", prompt)
}
	
