package binance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	baseURL        = "https://api.binance.com"
	testnetBaseURL = "https://testnet.binance.vision"
)

type RestClient struct {
	apiKey    string
	apiSecret string
	baseURL   string
	httpClient *http.Client
}

// AccountResponse from Binance API
type AccountResponse struct {
	CanTrade   bool    `json:"canTrade"`
	CanDeposit bool    `json:"canDeposit"`
	CanWithdraw bool   `json:"canWithdraw"`
	TotalAssetOfBTC string `json:"totalAssetOfBtc"`
	MakerCommission int `json:"makerCommission"`
	TakerCommission int `json:"takerCommission"`
	BuyerCommission int `json:"buyerCommission"`
	SellerCommission int `json:"sellerCommission"`
	CommissionRates struct {
		Maker string `json:"maker"`
		Taker string `json:"taker"`
		Buyer string `json:"buyer"`
		Seller string `json:"seller"`
	} `json:"commissionRates"`
	Balances []struct {
		Asset string `json:"asset"`
		Free string `json:"free"`
		Locked string `json:"locked"`
	} `json:"balances"`
	Permissions []string `json:"permissions"`
}

// OpenOrderResponse from Binance API
type OpenOrderResponse struct {
	Symbol string `json:"symbol"`
	OrderID int64 `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Price string `json:"price"`
	OrigQty string `json:"origQty"`
	ExecutedQty string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status string `json:"status"`
	TimeInForce string `json:"timeInForce"`
	Type string `json:"type"`
	Side string `json:"side"`
	StopPrice string `json:"stopPrice"`
	Time int64 `json:"time"`
	UpdateTime int64 `json:"updateTime"`
}

// KlineResponse from Binance API
type KlineResponse struct {
	OpenTime     int64  `json:"0"`
	Open         string `json:"1"`
	High         string `json:"2"`
	Low          string `json:"3"`
	Close        string `json:"4"`
	Volume       string `json:"5"`
	CloseTime    int64  `json:"6"`
	QuoteVolume  string `json:"7"`
	Trades       int64  `json:"8"`
	TakerBuyBase string `json:"9"`
	TakerBuyQuote string `json:"10"`
}

// NewRestClient creates a new Binance REST API client
func NewRestClient(apiKey, apiSecret string, testnet bool) *RestClient {
	baseURL := baseURL
	if testnet {
		baseURL = testnetBaseURL
	}
	return &RestClient{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetAccount fetches account information
func (c *RestClient) GetAccount(useServerTime bool) (*AccountResponse, error) {
	params := url.Values{}
	if useServerTime {
		ts, err := c.getServerTime()
		if err != nil {
			return nil, err
		}
		params.Add("timestamp", fmt.Sprintf("%d", ts))
	} else {
		params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}

	path := "/api/v3/account"
	sig := c.sign(params.Encode())
	params.Add("signature", sig)

	resp, err := c.doRequest("GET", path, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetAccount failed: %d - %s", resp.StatusCode, string(body))
	}

	var account AccountResponse
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, fmt.Errorf("decode account response: %w", err)
	}

	return &account, nil
}

// PlaceMarketOrder places a market order
func (c *RestClient) PlaceMarketOrder(symbol, side string, quantity float64) (orderID string, err error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("side", side)
	params.Add("type", "MARKET")
	params.Add("quantity", fmt.Sprintf("%.8f", quantity))
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))

	sig := c.sign(params.Encode())
	params.Add("signature", sig)

	path := "/api/v3/order"
	resp, err := c.doRequest("POST", path, params)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("PlaceMarketOrder failed: %d - %s", resp.StatusCode, string(body))
	}

	var orderResp struct {
		OrderID int64 `json:"orderId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orderResp); err != nil {
		return "", fmt.Errorf("decode order response: %w", err)
	}

	return fmt.Sprintf("%d", orderResp.OrderID), nil
}

// GetOpenOrders fetches open orders for a symbol
func (c *RestClient) GetOpenOrders(symbol string) ([]OpenOrderResponse, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))

	sig := c.sign(params.Encode())
	params.Add("signature", sig)

	path := "/api/v3/openOrders"
	resp, err := c.doRequest("GET", path, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetOpenOrders failed: %d - %s", resp.StatusCode, string(body))
	}

	var orders []OpenOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		return nil, fmt.Errorf("decode orders response: %w", err)
	}

	return orders, nil
}

// GetKlines fetches historical klines (candles)
func (c *RestClient) GetKlines(symbol, interval string, limit int) ([]KlineResponse, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("interval", interval)
	params.Add("limit", fmt.Sprintf("%d", limit))

	path := "/api/v3/klines"
	resp, err := c.doRequest("GET", path, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GetKlines failed: %d - %s", resp.StatusCode, string(body))
	}

	var klines []KlineResponse
	if err := json.NewDecoder(resp.Body).Decode(&klines); err != nil {
		return nil, fmt.Errorf("decode klines response: %w", err)
	}

	return klines, nil
}

// ValidateAPIKey validates the API key by making a test request
func (c *RestClient) ValidateAPIKey() (bool, error) {
	params := url.Values{}
	params.Add("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))

	sig := c.sign(params.Encode())
	params.Add("signature", sig)

	path := "/api/v3/account"
	resp, err := c.doRequest("GET", path, params)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

func (c *RestClient) doRequest(method, path string, params url.Values) (*http.Response, error) {
	u := fmt.Sprintf("%s%s", c.baseURL, path)

	if method == "GET" {
		u = fmt.Sprintf("%s?%s", u, params.Encode())
		req, err := http.NewRequest(method, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-MBX-APIKEY", c.apiKey)
		return c.httpClient.Do(req)
	}

	// POST request
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = io.NopCloser(nil)

	return c.httpClient.Do(req)
}

func (c *RestClient) sign(data string) string {
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *RestClient) getServerTime() (int64, error) {
	path := "/api/v3/time"
	resp, err := c.doRequest("GET", path, url.Values{})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var timeResp struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&timeResp); err != nil {
		return 0, err
	}

	return timeResp.ServerTime, nil
}
