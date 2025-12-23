package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	url := "https://mikanani.me/RSS/Classic"
	fmt.Printf("Fetching %s\n", url)

	// Create client with UA
	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\n", resp.Status)
	if len(body) > 1000 {
		fmt.Printf("Body (first 1000 chars): %s\n", string(body[:1000]))
	} else {
		fmt.Printf("Body: %s\n", string(body))
	}
}
