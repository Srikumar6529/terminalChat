package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

func getInput() (string, error) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("You > ")
	scanner.Scan()
	prompt := scanner.Text()
	if len(prompt) == 0 {
		return "", fmt.Errorf("Empty prompt")
	}
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

func getClaudeResponse(prompt string) (string, error) {

	maxTokens := 1024
	model := "claude-opus-5"

	url := "https://api.anthropic.com/v1/messages"

	err := godotenv.Load()
	if err != nil {
		return "", fmt.Errorf(".env file loading Failed| %w", err)
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	if apiKey == "" {
		return "", fmt.Errorf("apikey is not set")
	}

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

	client := &http.Client{}

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
}

func main() {
	prompt, err := getInput()
	//fmt.Println("You Entered:   ", prompt)
	if err != nil {
		fmt.Println(err)
		return
	}
	response, err := getClaudeResponse(prompt)
	if err != nil {
		//fmt.Println("ERROR OCCURED")
		fmt.Println(err)
		return
	}

	printResponse(response)
}
