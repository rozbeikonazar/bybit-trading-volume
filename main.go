package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	//"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/exp/rand"
	//"github.com/gorilla/websocket"
)

const (
	//wsURL            = "wss://stream.bybit.com/v5/public/spot"      // Mainnet WebSocket endpoint for spot
	symbol = "SCRUSDT" // Symbol to subscribe to
	//proxyURLStr      = "http://user207151:pe17rz@31.59.35.232:5919" // Set your proxy URL here
	symbol_short     = "SCR"
	TargetCommission = 20.0  // Target cumulative commission in USD
	CommissionRate   = 0.001 // Commission rate (e.g., 0.1%)
	MinOrderQty      = 0.1   // Minimum order quantity
	//TradeCooldown    = 2 * time.Second // Cooldown period between trades

)

//var latestPrice uint64 // Use atomic storage to safely read/write price

// Helper functions to set and get latestPrice safely
// func setLatestPrice(price float64) {
// 	atomic.StoreUint64(&latestPrice, math.Float64bits(price))
// }

// func getLatestPrice() float64 {
// 	return math.Float64frombits(atomic.LoadUint64(&latestPrice))
// }

// Message structure to hold the incoming WebSocket messages
type Message struct {
	Topic string `json:"topic"`
	TS    int64  `json:"ts"`
	Type  string `json:"type"`
	Data  struct {
		Symbol       string `json:"symbol"`
		LastPrice    string `json:"lastPrice"`
		HighPrice24h string `json:"highPrice24h"`
		LowPrice24h  string `json:"lowPrice24h"`
		PrevPrice24h string `json:"prevPrice24h"`
		Volume24h    string `json:"volume24h"`
		Turnover24h  string `json:"turnover24h"`
		Price24hPcnt string `json:"price24hPcnt"`
	} `json:"data"`
}

type AccountConfig struct {
	Token     string
	UserAgent string
	Proxy     string
}

// Define a struct to parse the wallet response
type WalletResponse struct {
	RetCode int `json:"retCode"`
	Result  struct {
		CoinList []struct {
			TokenID              string `json:"tokenId"`
			SpotAvailableBalance string `json:"spotAvailableBalance"`
		} `json:"coinList"`
	} `json:"result"`
}

func LoadAccounts(filename string) ([]AccountConfig, error) {
	// Open the file
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
	return accounts, nil
}

func getWalletBalance(account AccountConfig) (float64, float64, error) {
	apiURL := "https://api2.bybit.com/siteapi/unified/private/spot-walletbalance"
	body := map[string]interface{}{
		"symbolName": symbol,
	}

	// Set up the HTTP client with a proxy if provided
	client := &http.Client{}
	if account.Proxy != "" {
		proxyURL, err := url.Parse(account.Proxy)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid proxy URL: %v", err)
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return 0, 0, err
	}

	// Use "POST" method instead of "GET" since you are sending a body
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", account.UserAgent)
	req.Header.Set("Cookie", account.Token) // Use Cookie for authorization

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	// Print status code for debugging
	fmt.Printf("Response Status Code: %d\n", resp.StatusCode)

	// Read the response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read response body: %v", err)
	}
	fmt.Printf("Response Body: %s\n", string(bodyBytes))

	// Decode the response body into the expected structure
	var walletResponse WalletResponse
	if err := json.Unmarshal(bodyBytes, &walletResponse); err != nil {
		return 0, 0, fmt.Errorf("failed to decode JSON response: %v", err)
	}

	// Check for API errors in response
	if walletResponse.RetCode != 0 {
		return 0, 0, fmt.Errorf("failed to fetch wallet balance: ret_code %d", walletResponse.RetCode)
	}

	// Parse balance information with debug print statements
	var tokenBalance, usdtBalance float64
	for _, coin := range walletResponse.Result.CoinList {
		fmt.Printf("TokenID: %s, SpotAvailableBalance: %s\n", coin.TokenID, coin.SpotAvailableBalance) // Debugging output

		if coin.TokenID == symbol_short {
			tokenBalance, err = strconv.ParseFloat(coin.SpotAvailableBalance, 64)
			if err != nil {
				return 0, 0, err
			}
		} else if coin.TokenID == "USDT" {
			usdtBalance, err = strconv.ParseFloat(coin.SpotAvailableBalance, 64)
			if err != nil {
				return 0, 0, err
			}
		}
	}
	fmt.Printf("Parsed tokenBalance: %f, usdtBalance: %f\n", tokenBalance, usdtBalance) // Final parsed balances
	return tokenBalance, usdtBalance, nil
}
func createMarketOrder(side string, qty float64, account AccountConfig) error {
	// Create the order request payload

	body := map[string]interface{}{
		"symbol_id": "SCRUSDT", // Symbol to trade
		//"price":         fmt.Sprintf("%.4f", price), // Limit price
		"quantity":      fmt.Sprintf("%.1f", qty), // Order quantity
		"side":          side,                     // Side of the order (BUY or SELL)
		"type":          "market",                 // Order type
		"time_in_force": "GTC",                    // Time in force
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// Set up the HTTP client with a proxy if provided
	client := &http.Client{}
	if account.Proxy != "" {
		proxyURL, err := url.Parse(account.Proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %v", err)
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	req, err := http.NewRequest("POST", "https://api2-1.bybit.com/spot/api/order/create", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}

	// Use the Cookie for authorization
	req.Header.Set("Cookie", account.Token)
	//req.Header.Set("Content-Type", "application/json") // Set the content type
	req.Header.Set("User-Agent", account.UserAgent) // Set the User-Agent

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read the raw response body for error details
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API call failed: %s (status code: %d)", bodyBytes, resp.StatusCode)
	}

	// Check for successful response
	var createOrderResponse struct {
		RetCode int `json:"ret_code"`
		Result  struct {
			OrderID       string `json:"orderId"`
			ClientOrderID string `json:"clientOrderId"`
		} `json:"result"`
	}

	if err := json.Unmarshal(bodyBytes, &createOrderResponse); err != nil {
		return err
	}

	if createOrderResponse.RetCode != 0 {
		return fmt.Errorf("failed to create order: ret_code %d, details: %s", createOrderResponse.RetCode, string(bodyBytes))
	}
	return nil
}

// func connectToWebSocket(wsProxy, wsURL, symbol string, wg *sync.WaitGroup) {
// 	defer wg.Done() // Mark this goroutine as done when it exits

// 	dialer := websocket.Dialer{}
// 	if wsProxy != "" {
// 		proxyURL, err := url.Parse(wsProxy)
// 		if err != nil {
// 			log.Fatalf("Invalid proxy URL: %v", err)
// 		}
// 		dialer.Proxy = http.ProxyURL(proxyURL)
// 	}

// 	conn, _, err := dialer.Dial(wsURL, nil)
// 	if err != nil {
// 		log.Fatalf("Failed to connect to WebSocket: %v", err)
// 	}
// 	defer conn.Close()

// 	subscribeMessage := map[string]interface{}{
// 		"op":   "subscribe",
// 		"args": []string{"tickers." + symbol},
// 	}

// 	if err := conn.WriteJSON(subscribeMessage); err != nil {
// 		log.Fatalf("Failed to subscribe to ticker: %v", err)
// 	}

// 	stopChan := make(chan os.Signal, 1)
// 	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

// 	go func() {
// 		for {
// 			_, message, err := conn.ReadMessage()
// 			if err != nil {
// 				log.Printf("Error reading message: %v", err)
// 				break
// 			}

// 			var msg Message
// 			if err := json.Unmarshal(message, &msg); err != nil {
// 				log.Printf("Error unmarshaling message: %v", err)
// 				continue
// 			}

// 			if msg.Topic == "tickers."+symbol {
// 				price, err := strconv.ParseFloat(msg.Data.LastPrice, 64)
// 				if err != nil {
// 					log.Printf("Error parsing price: %v", err)
// 					continue
// 				}

// 				// Update latestPrice safely
// 				setLatestPrice(price)
// 			}
// 		}
// 	}()

// 	// Keep the connection alive until interrupted
// 	<-stopChan
// 	fmt.Println("Shutting down WebSocket gracefully...")
// }

func roundDownToPrecision(value float64, precision int) float64 {
	factor := math.Pow(10, float64(precision))
	return math.Floor(value*factor) / factor
}

func tradeLoop(ctx context.Context, account AccountConfig, accountIndex int) error {
	cumulativeCommission := 0.0

	for cumulativeCommission < TargetCommission {
		// Check if the context has been canceled (i.e., graceful shutdown requested)
		select {
		case <-ctx.Done():
			fmt.Printf("Account %d: Shutting down gracefully...\n", accountIndex)
			return nil
		default:
			// Trade actions for account
			fmt.Printf("Account %d: Starting trade cycle\n", accountIndex)

			// Step 1: Buy Order - Determine available USDT balance
			_, usdtBalance, err := getWalletBalance(account)
			if err != nil {
				fmt.Printf("Account %d: Error getting wallet balance: %v\n", accountIndex, err)
				return err
			}

			// Round down balance for precision
			qty := roundDownToPrecision(usdtBalance, 1)
			if qty < MinOrderQty {
				fmt.Printf("Account %d: Order quantity %f is below minimum required %f\n", accountIndex, qty, MinOrderQty)
				return nil
			}

			// Place a buy order using the available USDT balance
			err = createMarketOrder("buy", qty, account)
			if err != nil {
				fmt.Printf("Account %d: Error creating buy order: %v\n", accountIndex, err)
				return err
			}
			fmt.Printf("Account %d: Buy Order Created, bought with: $%.1f USDT\n", accountIndex, qty)

			// Update cumulative commission after buy order
			cumulativeCommission += qty * CommissionRate
			fmt.Printf("Account %d: Cumulative Commission after buy: $%.5f\n", accountIndex, cumulativeCommission)

			// Step 2: Sell Order - Determine available SCR balance
			scrBalance, _, err := getWalletBalance(account)
			if err != nil {
				fmt.Printf("Account %d: Error getting wallet balance: %v\n", accountIndex, err)
				return err
			}

			// Round down SCR balance for precision
			scrQty := roundDownToPrecision(scrBalance, 1)
			if scrQty < MinOrderQty {
				fmt.Printf("Account %d: Sell quantity %f is below minimum required %f\n", accountIndex, scrQty, MinOrderQty)
				return nil
			}

			// Place a sell order using the available SCR balance
			err = createMarketOrder("sell", scrQty, account)
			if err != nil {
				fmt.Printf("Account %d: Error creating sell order: %v\n", accountIndex, err)
				return err
			}
			fmt.Printf("Account %d: Sell Order Created, sold %.1f SCR\n", accountIndex, scrQty)

			// Update cumulative commission after sell order
			cumulativeCommission += qty * CommissionRate // USDT quantity to avoid bugs with WebSocket
			fmt.Printf("Account %d: Cumulative Commission after sell: $%.5f\n", accountIndex, cumulativeCommission)

			// Pause briefly to avoid rate limits
			tradeCooldown := time.Duration(rand.Intn(5)+1) * time.Second
			time.Sleep(tradeCooldown)

			// Check if cumulative commission has reached the target after each trade
			if cumulativeCommission >= TargetCommission {
				fmt.Printf("Account %d: Target cumulative commission reached, stopping trading loop.\n", accountIndex)
				return nil
			}
		}
	}
	return nil
}

func main() {

	accounts, err := LoadAccounts("accounts.json")
	if err != nil {
		log.Fatal("Failed to load accounts:", err)
	}

	// Set up OS signal capturing for graceful shutdown
	sigs := make(chan os.Signal, 1)
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Define the staggered delay times in minutes
	// 0, 3, and 6 minutes for each account
	staggerDelayMin := 30 * time.Second
	staggerDelayMax := 2 * time.Minute
	for i, account := range accounts {
		wg.Add(1)

		// Calculate the delay for the current account
		delay := time.Duration(rand.Intn(int(staggerDelayMax-staggerDelayMin)) + int(staggerDelayMin))

		// Sleep for the calculated delay
		go func(acc AccountConfig, accIndex int, d time.Duration) {
			time.Sleep(d) // Wait for the randomized delay
			defer wg.Done()

			if err := tradeLoop(ctx, acc, accIndex); err != nil {
				fmt.Printf("Trade loop for account %d ended with error: %v\n", accIndex, err)
			}
		}(account, i+1, delay)
	}
	// Wait for a shutdown signal
	<-sigs
	fmt.Println("Shutdown signal received")

	// Trigger graceful shutdown by cancelling context
	cancel()

	// Wait for all goroutines to complete
	wg.Wait()

	fmt.Println("Application stopped")

}
