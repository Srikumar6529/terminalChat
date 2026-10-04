package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

func getInput(scanner *bufio.Scanner) (string, error) {

	fmt.Print("You > ")
	successful := scanner.Scan()
	if !successful {
		if scanner.Err() == nil {
			return "", io.EOF
		}
		return "", scanner.Err()
	}
	prompt := scanner.Text()
	return prompt, nil
}

type RequestBody struct {
	MaxTokens int                 `json:"max_tokens"`
	Messages  []map[string]string `json:"messages"`
	Model     string              `json:"model"`
}

type ResponseBody struct {
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func getClaudeResponse(prompt, apiKey string, client *http.Client) (string, error) {

	maxTokens := 1024
	model := "claude-opus-5"

	url := "https://api.anthropic.com/v1/messages"

	inputMessages := map[string]string{
		"content": prompt,
		"role":    "user",
	}

	messagesArray := []map[string]string{
		inputMessages,
	}

	reqBody := RequestBody{
		MaxTokens: maxTokens,
		Messages:  messagesArray,
		Model:     model,
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("Failed to encode request body to JSON| %w", err)
	}
	//fmt.Println(string(reqBodyJson))
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBodyJSON))
	if err != nil {
		return "", fmt.Errorf("Unable to make request| %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed | %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(string(data))
		return "", fmt.Errorf("Unsuccessful request | %v", resp.Status)
	}

	var respBody ResponseBody

	err = json.NewDecoder(resp.Body).Decode(&respBody)
	if err != nil {
		return "", err
	}
	for _, block := range respBody.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("no text block in response body")

}

func printResponse(response string) {
	fmt.Println("Claude Reponse > ", response)
	fmt.Println()
}

func main() {
	//creating scanner object
	scanner := bufio.NewScanner(os.Stdin)
	//Loading env variables and getting the apiKey
	err := godotenv.Load()
	if err != nil {
		fmt.Println(".env file loading Failed| ", err)
		return
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	if apiKey == "" {
		fmt.Println("apikey is not set")
		return
	}
	//Creating the http Client for sending requests to the anthropic server
	client := &http.Client{}

	// main loop
	for {
		//first lets get the input
		prompt, err := getInput(scanner)

		if err == io.EOF {
			fmt.Println("\nGood Bye!")
			return
		}
		if err != nil {
			fmt.Println(err)
			return
		}

		if len(prompt) == 0 {
			fmt.Println("Empty prompt, Try again.")
			continue
		}

		//now lets check if the input is quit
		if strings.ToLower(strings.TrimSpace(prompt)) == "quit" {
			fmt.Println("Good Bye!")
			return
		}

		//if its a valid prompt lets send it to claude
		response, err := getClaudeResponse(prompt, apiKey, client)
		if err != nil {
			//fmt.Println("ERROR OCCURED")
			fmt.Println(err)
			continue
		}

		printResponse(response)

	}

}
