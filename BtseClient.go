package main

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"gopkg.in/ini.v1"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DATA STRUCTURES

type Subscription struct {
	Op   string   `json:"op"`
	Args []string `json:"args"`
}

type Market struct {
	Base              string  `json:"base"`
	Symbol            string  `json:"symbol"`
	Active            bool    `json:"active"`
	MinPriceIncrement float64 `json:"minPriceIncrement"`
	MinSizeIncrement  float64 `json:"minSizeIncrement"`
	ContractSize      float64 `json:"contractSize"`
	MinOrderSize      float64 `json:"minOrderSize"`
}

type WsOrderbookResp struct {
	Topic string        `json:"topic"`
	Data  OrderbookData `json:"data"`
}

type OrderbookData struct {
	Bids       [][]string `json:"bids"`
	Asks       [][]string `json:"asks"`
	SeqNum     int64      `json:"seqNum"`
	PrevSeqNum int64      `json:"prevSeqNum"`
	Type       string     `json:"type"`
	Symbol     string     `json:"symbol"`
	Timestamp  int64      `json:"timestamp"`
}

type Config struct {
	ApiKey    string
	ApiSecret string
}

type OrderBook struct {
	Symbol          string
	TsMs            float64
	Timestamp       float64
	TopBid          [2]float64
	TopBidTimestamp float64
	Bids            map[string]string
	TopAsk          [2]float64
	TopAskTimestamp float64
	Asks            map[string]string
}

// VARIABLES AND CONSTANTS

const (
	configLocation = "config.ini"
	exchangeName   = "BTSE"
	wsPublicAddr   = "ws.btse.com"
	wsPublicPath   = "/ws/oss/futures"
	getMarketsUrl  = "https://api.btse.com/futures/api/v2.1/market_summary"
)

var Instruments = make(map[string]interface{})

var Markets = getMarkets(getMarketsUrl)

var config Config

var orderbook = make(map[string]OrderBook)

// FUNCTIONS

func getApiKeys() {

	cfg, err := ini.Load(configLocation)
	if err != nil {
		log.Fatalf("Fail to read file: %v", err)
	}

	section, err := cfg.GetSection(exchangeName)
	if err != nil {
		log.Fatalf("Fail to get section: %v", err)
	}

	apiKey, err := section.GetKey("API_KEY")
	if err != nil {
		log.Fatalf("Fail to get 'API_KEY': %v", err)
	}

	apiSecret, err := section.GetKey("API_SECRET")
	if err != nil {
		log.Fatalf("Fail to get 'API_SECRET': %v", err)
	}

	// Now you can assign these values to your config struct
	config.ApiKey = apiKey.String()
	config.ApiSecret = apiSecret.String()

	fmt.Println("Config:", config)

}

func generateSignature(path, nonce, data string) string {

	// Combine path, nonce, and data into a single message.
	message := path + nonce + data
	// Convert apiSecret to bytes and prepare the hasher.
	// Note: Assuming apiSecret is in "latin-1", but in practice, Go handles
	// strings as UTF-8. Direct conversion to []byte should be equivalent for
	// the common subset of latin-1 and UTF-8.
	hasher := hmac.New(sha512.New384, []byte(config.ApiSecret))
	// Write message to the hasher and compute the HMAC signature.
	hasher.Write([]byte(message))
	signature := hasher.Sum(nil)
	// Return the hexadecimal representation of the signature.
	return hex.EncodeToString(signature)
}

func getPrivateHeaders(req *http.Request, path string, data map[string]interface{}) {
	// Convert data map to JSON string if not empty; else use an empty string.
	var jsonStr string
	if data != nil && len(data) > 0 {
		jsonData, err := json.Marshal(data)
		if err != nil {
			fmt.Println("Error marshalling data:", err)
			return
		}
		jsonStr = string(jsonData)
	}

	// Generate nonce as current Unix timestamp in milliseconds plus a random offset.
	nonce := strconv.FormatInt(time.Now().UnixNano()/1e6+int64(rand.Intn(201)-100), 10)

	// Generate signature using the path, nonce, and JSON data.
	signature := generateSignature(path, nonce, jsonStr)

	// Update request headers with authentication details.
	req.Header.Set("request-api", config.ApiKey)
	req.Header.Set("request-nonce", nonce)
	req.Header.Set("request-sign", signature)
}

func getPricePrecision(tickSize float64) int {

	var pricePrecision int
	tickSizeStr := fmt.Sprintf("%v", tickSize)
	if strings.Contains(tickSizeStr, ".") {
		parts := strings.Split(tickSizeStr, ".")
		pricePrecision = len(parts[1])
	} else if strings.Contains(tickSizeStr, "-") {
		parts := strings.Split(tickSizeStr, "-")
		pricePrecision, _ = strconv.Atoi(parts[1])
	} else {
		pricePrecision = 0
	}
	return pricePrecision
}

func getQuantityPrecision(stepSize float64) int {

	var quantityPrecision int
	if strings.Contains(fmt.Sprintf("%v", stepSize), ".") {
		parts := strings.Split(fmt.Sprintf("%v", stepSize), ".")
		quantityPrecision = len(parts[1])
	} else {
		quantityPrecision = 1
	}
	return quantityPrecision
}

func updateInstruments(market Market) {

	tickSize := market.MinSizeIncrement
	pricePrecision := getPricePrecision(tickSize)
	contractSize := market.ContractSize
	stepSize := tickSize * contractSize
	quantityPrecision := getQuantityPrecision(stepSize)
	minSize := market.MinOrderSize * contractSize

	Instruments[market.Symbol] = map[string]interface{}{
		"contract_value":     contractSize,
		"tick_size":          tickSize,
		"step_size":          stepSize,
		"quantity_precision": quantityPrecision,
		"price_precision":    pricePrecision,
		"min_size":           minSize,
	}
}

func getMarkets(url string) map[string]string {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("Error fetching markets:", err)
		return nil
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return nil
	}

	var markets []Market
	err = json.Unmarshal(body, &markets)
	if err != nil {
		fmt.Println("Error unmarshalling JSON:", err)
		return nil
	}

	marketMap := make(map[string]string)
	for _, market := range markets {
		if market.Active && strings.Contains(market.Symbol, "PFC") {
			marketMap[market.Base] = market.Symbol
			updateInstruments(market)
		}
	}
	//fmt.Println("Instruments:", Instruments)
	return marketMap
}

func connect(endpoint string) (*websocket.Conn, error) {
	u := url.URL{Scheme: "wss", Host: wsPublicAddr, Path: endpoint}
	fmt.Println(u)
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	return c, err
}

func subscribeOrderbook(conn *websocket.Conn, channel []string) {
	// Subscribe to public channel
	subscription := Subscription{
		Op:   "subscribe",
		Args: channel, // Replace with your actual data
	}
	// Marshal your subscription data into JSON
	message, err := json.Marshal(subscription)
	if err != nil {
		log.Fatal("Marshal error:", err)
	}
	// Send the message
	if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
		log.Fatal("WriteMessage error:", err)
	}
	//message := fmt.Sprintf(`{"op": "subscribe", "args": ["%s"]}`, channel)
	//return conn.WriteMessage(websocket.TextMessage, []byte(message))
}

func pingWs(conn *websocket.Conn) {
	for {
		err := conn.WriteMessage(websocket.PingMessage, nil)
		if err != nil {
			log.Println("ping:", err)
		}
		time.Sleep(25 * time.Second)
	}
}

func readMessages(conn *websocket.Conn) {
	defer conn.Close()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}

		var msg WsOrderbookResp
		err = json.Unmarshal([]byte(message), &msg)
		if err != nil {
			log.Fatal(err)
		}
		UpdateOrderBook(msg)
		//PRINTS OF WS PING
		//log.Printf("recv: %s", msg)
		currentTime := time.Now()
		timestamp := currentTime.UnixNano() / int64(time.Millisecond)
		fmt.Println("WS ping:", timestamp-msg.Data.Timestamp)
	}
}

func processBids(data WsOrderbookResp, newOb OrderBook, tsOb float64) OrderBook {
	for _, bid := range data.Data.Bids {
		price, _ := strconv.ParseFloat(bid[0], 64)
		size, _ := strconv.ParseFloat(bid[1], 64)

		if bid[1] == "0" {
			delete(newOb.Bids, bid[0])
			newOb = findHighestBidPrice(newOb, tsOb)
		} else {
			newOb.Bids[bid[0]] = bid[1]
		}

		if price >= newOb.TopBid[0] {
			newOb.TopBid = [2]float64{price, size}
			newOb.TopBidTimestamp = tsOb
		}
	}
	return newOb
}

func processAsks(data WsOrderbookResp, newOb OrderBook, tsOb float64) OrderBook {
	for _, ask := range data.Data.Asks {
		price, _ := strconv.ParseFloat(ask[0], 64)
		size, _ := strconv.ParseFloat(ask[1], 64)

		if ask[1] == "0" {
			delete(newOb.Asks, ask[0])
			newOb = findLowestAskPrice(newOb, tsOb)
		} else {
			newOb.Asks[ask[0]] = ask[1]
		}

		if price <= newOb.TopAsk[0] {
			newOb.TopAsk = [2]float64{price, size}
			newOb.TopAskTimestamp = tsOb
		}
	}
	return newOb
}

func findHighestBidPrice(orderBook OrderBook, tsOb float64) OrderBook {
	firstIteration := true
	highestPrice := 0.0
	var sizeStr string
	for priceStr := range orderBook.Bids {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			fmt.Println("TopBid price cannot be converted to float64:", priceStr)
			continue
		}
		if firstIteration || price > highestPrice {
			highestPrice = price
			sizeStr, _ = orderBook.Bids[priceStr]
			firstIteration = false
		}
	}
	if firstIteration {
		fmt.Println("No asks available to get topBid")
		return orderBook
	}
	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		fmt.Println("TopBid size cannot be converted to float64:", sizeStr)
		return orderBook
	}
	orderBook.TopBid = [2]float64{highestPrice, size}
	orderBook.TopBidTimestamp = tsOb
	return orderBook
}

func findLowestAskPrice(orderBook OrderBook, tsOb float64) OrderBook {
	firstIteration := true
	lowestPrice := 999999999999999.0
	var sizeStr string
	for priceStr := range orderBook.Asks {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			fmt.Println("TopAsk price cannot be converted to float64:", priceStr)
			continue
		}
		if firstIteration || price < lowestPrice {
			lowestPrice = price
			sizeStr, _ = orderBook.Asks[priceStr]
			firstIteration = false
		}
	}
	if firstIteration {
		fmt.Println("No asks available to get topAsk")
		return orderBook
	}
	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		fmt.Println("TopAsk size cannot be converted to float64:", sizeStr)
		return orderBook
	}
	orderBook.TopAsk = [2]float64{lowestPrice, size}
	orderBook.TopAskTimestamp = tsOb
	return orderBook
}

func UpdateOrderBook(data WsOrderbookResp) {
	tsMs := float64(time.Now().UnixNano()) / 1e9
	tsOb := float64(data.Data.Timestamp) / 1000

	symbol := data.Data.Symbol
	newOb, exists := orderbook[symbol]
	if !exists {
		newOb = OrderBook{
			Symbol: symbol,
			Bids:   make(map[string]string),
			Asks:   make(map[string]string),
		}
	}
	newOb.TsMs = tsMs
	newOb.Timestamp = tsOb

	newOb = processBids(data, newOb, tsOb)
	newOb = processAsks(data, newOb, tsOb)

	// Update global orderbook
	orderbook[symbol] = newOb
	fmt.Println("New OB:", newOb.Symbol, newOb)
	// Additional logic based on `side` and `finder` needs to be converted to Go's concurrency model,
	// such as using goroutines and channels if necessary.
}

func runOrderbookWebsocket(endpoint string) {
	connPublic, err := connect(endpoint)
	if err != nil {
		log.Fatal("connect:", err)
	}

	var subMarkets []string

	for _, market := range Markets {
		sub := "update:" + market + "_0"
		subMarkets = append(subMarkets, sub)
	}

	subscribeOrderbook(connPublic, subMarkets)
	go readMessages(connPublic)
	go pingWs(connPublic)
	defer connPublic.Close()
	for {
		time.Sleep(time.Second)
	}
}

func main() {
	// Example usage
	getApiKeys()
	// Handling messages for public connection
	go runOrderbookWebsocket(wsPublicPath)
	//fmt.Println("Markets:", Markets)
	// Prevent the main goroutine from exiting immediately
	for {
		time.Sleep(time.Second)
	}
}
