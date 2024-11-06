package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
)

// helpers

func LoadAccounts(filename string) ([]AccountConfig, error) {
	// Open the file
	fmt.Println("Loading accounts")
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Initialize a slice to store the accounts
	var accounts []AccountConfig

	// Decode JSON data directly into the slice
	if err := json.NewDecoder(file).Decode(&accounts); err != nil {
		return nil, fmt.Errorf("could not parse JSON: %v", err)
	}
	fmt.Println("ACCOUNTS LOADED")
	return accounts, nil
}

func setProxy(account AccountConfig) (*http.Client, error) {
	client := &http.Client{}
	if account.Proxy != "" {
		proxyURL, err := url.Parse(account.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy: %s, error: %v", account.Proxy, err)
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}
	return client, nil
}

func roundDownToPrecision(value float64, precision int) float64 {
	factor := math.Pow(10, float64(precision))
	return math.Floor(value*factor) / factor
}

func setHeaders(req *http.Request, account AccountConfig) {
	req.Header.Set("User-Agent", account.UserAgent)
	req.Header.Set("Cookie", account.Token)
}

// END OF HELPERS
