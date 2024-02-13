package tests

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

func getURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Got err 2:", err)
		return nil, err
	}
	return body, nil
}

const (
	urlBtse     = "https://api.btse.com/futures/api/v2.1/orderbook?symbol=ETHPFC&depth=10"
	urlWhitebit = "https://whitebit.com/api/v4/public/orderbook/BTC_PERP?limit=10"
)

var btsePings []float64
var whitebitPings []float64

func calculateAverage(pings []float64) float64 {
	var sum float64
	for _, ping := range pings {
		sum += ping
	}
	return sum / float64(len(pings))
}

func main() {
	for {

		start := time.Now()
		_, err := getURL(urlBtse)
		if err != nil {
			fmt.Println("Error fetching URL:", err)
			continue
		}
		elapsed := time.Since(start).Seconds()
		btsePings = append(btsePings, elapsed)
		// 		fmt.Printf("Time taken for %s: %.2fs\n", url_btse, elapsed)

		start = time.Now()
		_, err = getURL(urlWhitebit)
		if err != nil {
			fmt.Println("Error fetching URL:", err)
			continue
		}
		elapsed = time.Since(start).Seconds()
		whitebitPings = append(whitebitPings, elapsed)
		// 		fmt.Printf("Time taken for %s: %.2fs\n", url_whitebit, elapsed)

		if len(btsePings) > 0 {
			fmt.Printf("Average time for BTSE: %.4fs\n", calculateAverage(btsePings))
		}
		if len(whitebitPings) > 0 {
			fmt.Printf("Average time for Whitebit: %.4fs\n", calculateAverage(whitebitPings))
		}

		time.Sleep(1 * time.Second)
	}
}
