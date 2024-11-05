package main

// import (
// 	"encoding/json"
// 	"fmt"
// 	"os"
// )

// func LoadAccounts(filename string) ([]AccountConfig, error) {
// 	// Open the file
// 	file, err := os.Open(filename)
// 	if err != nil {
// 		return nil, fmt.Errorf("could not open file: %v", err)
// 	}
// 	defer file.Close()

// 	// Initialize a slice to store the accounts
// 	var accounts []AccountConfig

// 	// Decode JSON data directly into the slice
// 	if err := json.NewDecoder(file).Decode(&accounts); err != nil {
// 		return nil, fmt.Errorf("could not parse JSON: %v", err)
// 	}
// 	return accounts, nil
// }
