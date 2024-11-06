package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/signal"
	"syscall"

	//"log"

	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/exp/rand"
	//"github.com/gorilla/websocket"
)

const (
	//wsURL            = "wss://stream.bybit.com/v5/public/spot"      // Mainnet WebSocket endpoint for spot
	symbol = "GRASSUSDT" // Symbol to subscribe to
	//proxyURLStr      = "http://user207151:pe17rz@31.59.35.232:5919" // Set your proxy URL here
	symbol_short     = "GRASS"
	TargetCommission = 20.0  // Target cumulative commission in USD
	CommissionRate   = 0.001 // Commission rate (e.g., 0.1%)
	precision        = 1
)

var transferThresholds = []float64{5, 10, 15}
var staggerDelayMin = 30 * time.Second
var staggerDelayMax = 2 * time.Minute

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
type SpotWalletResponse struct {
	RetCode int `json:"retCode"`
	Result  struct {
		CoinList []struct {
			TokenID              string `json:"tokenId"`
			SpotAvailableBalance string `json:"spotAvailableBalance"`
		} `json:"coinList"`
	} `json:"result"`
}

func getSpotWalletBalance(account AccountConfig) (float64, float64, error) {
	apiURL := "https://api2.bybit.com/siteapi/unified/private/spot-walletbalance"
	body := map[string]interface{}{
		"symbolName": symbol,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return 0, 0, err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return 0, 0, err
	}
	// Set up the HTTP client with a proxy if provided
	client, err := setProxy(account)
	if err != nil {
		return 0, 0, err
	}
	// Set up Headers
	setHeaders(req, account)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	// Print status code for debugging
	//fmt.Printf("Response Status Code: %d\n", resp.StatusCode)

	// Read the response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read response body: %v", err)
	}
	//fmt.Printf("Response Body: %s\n", string(bodyBytes))

	// Decode the response body into the expected structure
	var walletResponse SpotWalletResponse
	if err := json.Unmarshal(bodyBytes, &walletResponse); err != nil {
		return 0, 0, fmt.Errorf("failed to decode JSON response: %v", err)
	}

	// Check for API errors in response
	if walletResponse.RetCode != 0 {
		return 0, 0, fmt.Errorf("failed to fetch wallet balance: ret_code %d, PROXY: %s. MOST LIKELY SESSION DIED", walletResponse.RetCode, account.Proxy)
	}

	// Parse balance information with debug print statements
	var tokenBalance, usdtBalance float64
	for _, coin := range walletResponse.Result.CoinList {
		//fmt.Printf("TokenID: %s, SpotAvailableBalance: %s\n", coin.TokenID, coin.SpotAvailableBalance) // Debugging output

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
	//fmt.Printf("Parsed tokenBalance: %f, usdtBalance: %f\n", tokenBalance, usdtBalance) // Final parsed balances
	return tokenBalance, usdtBalance, nil
}
func createMarketOrder(side string, qty float64, account AccountConfig) error {
	// Create the order request payload
	body := map[string]interface{}{
		"symbol_id": symbol,
		//"price":         fmt.Sprintf("%.4f", price), // Limit price
		"quantity":      fmt.Sprintf("%v", qty), // Order quantity
		"side":          side,                   // Side of the order (BUY or SELL)
		"type":          "market",               // Order type
		"time_in_force": "GTC",                  // Time in force
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// Set up the HTTP client with a proxy if provided

	req, err := http.NewRequest("POST", "https://api2-1.bybit.com/spot/api/order/create", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}

	client, err := setProxy(account)
	if err != nil {
		return err
	}

	setHeaders(req, account)

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

func pollBalance(account AccountConfig, accountType, sCoin, toAccountType string) (string, string, string, error) {
	pollBalanceUrl := fmt.Sprintf("https://api2.bybit.com/v3/private/asset/account/coin-balance?accountType=%s&sCoin=%s&toAccountType=%s", accountType, sCoin, toAccountType)

	// Create a new HTTP GET request
	req, err := http.NewRequest("GET", pollBalanceUrl, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create request: %v", err)
	}
	client, err := setProxy(account)
	if err != nil {
		return "", "", "", err
	}

	// Set the necessary headers
	setHeaders(req, account)

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to poll balance: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read response body: %v", err)
	}

	// Parse the JSON response
	var result struct {
		Result struct {
			AccountId string `json:"accountId"`
			Balance   struct {
				SCoin           string `json:"sCoin"`
				TransferBalance string `json:"transferBalance"`
			} `json:"balance"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %v", err)
	}

	return result.Result.AccountId, result.Result.Balance.SCoin, result.Result.Balance.TransferBalance, nil
}

// Sends a POST request to Bybit's transfer endpoint
func executeTransfer(account AccountConfig, amount, fromAccountId, toAccountId, sCoin string) error {
	// Prepare the payload for the transfer request
	payload := map[string]string{
		"amount":          amount,
		"fromAccountType": "ACCOUNT_TYPE_FUND",
		"from_account_id": fromAccountId,
		"sCoin":           sCoin,
		"toAccountType":   "ACCOUNT_TYPE_UNIFIED",
		"to_account_id":   toAccountId,
	}

	// Convert the payload to JSON format
	jsonPayload, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api2.bybit.com/v3/private/asset/transfer", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create transfer request: %v", err)
	}

	client, err := setProxy(account)
	if err != nil {
		return err
	}

	setHeaders(req, account)

	// Execute the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute transfer: %v", err)
	}
	defer resp.Body.Close()

	// Check for a successful status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}

	fmt.Println("Transfer successful.")
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

func tradeLoop(ctx context.Context, account AccountConfig, accountIndex int) error {
	cumulativeCommission := 0.0
	nextThresholdIndex := 0

	for cumulativeCommission < TargetCommission {
		// Check if the context has been canceled (i.e., graceful shutdown requested)
		select {
		case <-ctx.Done():
			fmt.Printf("Account %d: Shutting down gracefully...\n", accountIndex)
			return nil
		default:
			// Trade actions for account

			//fmt.Printf("Account %d: Starting trade cycle\n", accountIndex)

			// Step 1: Buy Order - Determine available USDT balance
			_, usdtBalance, err := getSpotWalletBalance(account)
			if err != nil {
				fmt.Printf("Account %d: Error getting wallet balance: %v\n", accountIndex, err)
				return err
			}
			fmt.Printf("Account %d: usdtBalance: %.4f\n", accountIndex, usdtBalance)
			// Round down balance for precision
			// Hardcoding the precision to 1 for buying tokens in USDT. This is because USDT purchases
			// universally allow precision in amounts (e.g., buying in fractional quantities like 0.1 or 0.01 USDT).
			// However, some tokens (like X) only allow integer quantities when selling, which requires precision = 0 for sell trades.
			// To handle this difference, we use precision = 1 here for buying and manage sell precision separately as needed.
			qty := roundDownToPrecision(usdtBalance, 1) // Precision is hardcoded for USDT buys; tokens universally allow it.
			// Place a buy order using the available USDT balance
			err = createMarketOrder("buy", qty, account)
			if err != nil {
				fmt.Printf("Account %d: Error creating buy order: %v\n", accountIndex, err)
				return err
			}
			fmt.Printf("Account %d: Buy Order Created, bought with: $%.1f USDT\n", accountIndex, qty)

			// Update cumulative commission after buy order
			cumulativeCommission += qty * CommissionRate

			// this line for debug purposes
			//fmt.Printf("Account %d: Cumulative Commission after buy: $%.5f\n", accountIndex, cumulativeCommission)

			// Step 2: Sell Order - Determine available SCR balance
			tokenBalance, _, err := getSpotWalletBalance(account)
			if err != nil {
				fmt.Printf("Account %d: Error getting wallet balance: %v\n", accountIndex, err)
				return err
			}

			tokenQty := roundDownToPrecision(tokenBalance, precision)

			// Place a sell order using the available Token Balance
			err = createMarketOrder("sell", tokenQty, account)
			if err != nil {
				fmt.Printf("Account %d: Error creating sell order: %v\n", accountIndex, err)
				return err
			}
			fmt.Printf("Account %d: Sell Order Created, sold $%s qty:%.1f\n", accountIndex, symbol, tokenQty)

			// Update cumulative commission after sell order
			cumulativeCommission += qty * CommissionRate // USDT quantity to avoid bugs with WebSocket
			fmt.Printf("Account %d: Cumulative Commission after BUY/SELL cycle: $%.5f\n", accountIndex, cumulativeCommission)

			// Pause briefly to avoid rate limits
			tradeCooldown := time.Duration(500+rand.Intn(1500)) * time.Millisecond

			time.Sleep(tradeCooldown)

			if nextThresholdIndex < len(transferThresholds) && cumulativeCommission >= transferThresholds[nextThresholdIndex] {
				fmt.Printf("Account %d: Reached cumulative trading volume of $%.2f, polling for transfer...\n", accountIndex, transferThresholds[nextThresholdIndex])

				// Step 1: Poll both endpoints to get fromAccountId and toAccountId
				fromAccountId, sCoin, transferBalance, err := pollBalance(account, "ACCOUNT_TYPE_FUND", "USDT", "ACCOUNT_TYPE_UNIFIED")
				if err != nil {
					fmt.Printf("Account %d: Error polling fund account balance: %v\n", accountIndex, err)
					return err
				}

				toAccountId, _, _, err := pollBalance(account, "ACCOUNT_TYPE_UNIFIED", "USDT", "ACCOUNT_TYPE_FUND")
				if err != nil {
					fmt.Printf("Account %d: Error polling unified account balance: %v\n", accountIndex, err)
					return err
				}
				transferBalanceFloat, err := strconv.ParseFloat(transferBalance, 64)
				if err != nil {
					fmt.Printf("Account %d: Error converting transfer balance to float: %v\n", accountIndex, err)
					return err
				}

				if transferBalanceFloat != 0 {
					// Execute the transfer only if the transferBalance is not zero
					err = executeTransfer(account, transferBalance, fromAccountId, toAccountId, sCoin)
					if err != nil {
						fmt.Printf("Account %d: Error executing transfer: %v\n", accountIndex, err)
						return err
					}
					fmt.Printf("Account %d: Transfer executed successfully.\n", accountIndex)
				} else {
					fmt.Printf("Account %d: Transfer balance is zero, skipping transfer.\n", accountIndex)
				}

				// Move to the next threshold
				nextThresholdIndex++
			}

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
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	go func() {
		<-sigs
		fmt.Println("Shutdown signal received")
		cancel() // Cancel the context to signal all trade loops to stop
	}()
	// Wait for all goroutines to complete gracefully
	wg.Wait()

	fmt.Println("Application stopped gracefully")
}
