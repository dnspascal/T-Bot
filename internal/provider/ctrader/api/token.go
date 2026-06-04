package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type tokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ErrorCode    any    `json:"errorCode"`
	Description  any    `json:"description"`
}

func RefreshToken(clientID, clientSecret, refreshToken string) (accessToken, newRefreshToken string, err error) {
	params := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	resp, err := http.Get("https://openapi.ctrader.com/apps/token?" + params.Encode())
	if err != nil {
		return "", "", fmt.Errorf("RefreshToken: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", "", fmt.Errorf("RefreshToken decode: %w", err)
	}
	if code, ok := tr.ErrorCode.(string); ok && code != "" {
		return "", "", fmt.Errorf("RefreshToken: %s: %v", code, tr.Description)
	}
	if tr.AccessToken == "" {
		return "", "", fmt.Errorf("RefreshToken: empty access token in response")
	}
	return tr.AccessToken, tr.RefreshToken, nil
}
