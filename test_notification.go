package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	// Test the JSON payload structure from the SQL trigger
	sqlPayload := `{
		"key": "test/example",
		"ts": "2025-09-10T19:00:00+02:00",
		"value": "test_value",
		"revision": 123,
		"operation": "UPDATE"
	}`

	var notification struct {
		Key       string  `json:"key"`
		Ts        string  `json:"ts"`
		Value     *string `json:"value"`
		Revision  *int64  `json:"revision"`
		Operation string  `json:"operation"`
	}

	err := json.Unmarshal([]byte(sqlPayload), &notification)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Parsed successfully:\n")
	fmt.Printf("Key: %s\n", notification.Key)
	fmt.Printf("Ts: %s\n", notification.Ts)
	fmt.Printf("Value: %v\n", notification.Value)
	fmt.Printf("Revision: %v\n", notification.Revision)
	fmt.Printf("Operation: %s\n", notification.Operation)
}
