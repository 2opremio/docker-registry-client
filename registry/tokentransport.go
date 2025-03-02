package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type TokenTransport struct {
	Transport     http.RoundTripper
	ClientTimeout time.Duration
	Username      string
	Password      string
	client        *http.Client
}

func (t *TokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if authService := isTokenDemand(resp); authService != nil {
		resp.Body.Close()
		resp, err = t.authAndRetry(authService, req)
	}
	return resp, err
}

type authToken struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

func (t *TokenTransport) authAndRetry(authService *authService, req *http.Request) (*http.Response, error) {
	token, err := t.auth(authService)
	if err != nil {
		return nil, err
	}

	retryResp, err := t.retry(req, token)
	return retryResp, err
}

func (t *TokenTransport) auth(authService *authService) (string, error) {
	authReq, err := authService.Request(t.Username, t.Password)
	if err != nil {
		return "", err
	}

	if t.client == nil {
		t.client = &http.Client{
			Transport: t.Transport,
			Timeout:   t.ClientTimeout,
		}
	}

	response, err := t.client.Do(authReq)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", err
	}

	var authToken authToken
	decoder := json.NewDecoder(response.Body)
	err = decoder.Decode(&authToken)
	if err != nil {
		return "", err
	}

	if authToken.Token == "" {
		return authToken.AccessToken, nil
	} else {
		return authToken.Token, nil
	}
}

func (t *TokenTransport) retry(req *http.Request, token string) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	resp, err := t.Transport.RoundTrip(req)
	return resp, err
}

type authService struct {
	Realm   string
	Service string
	Scope   string
}

func (authService *authService) Request(username, password string) (*http.Request, error) {
	url, err := url.Parse(authService.Realm)
	if err != nil {
		return nil, err
	}

	q := url.Query()
	q.Set("service", authService.Service)
	if authService.Scope != "" {
		q.Set("scope", authService.Scope)
	}
	url.RawQuery = q.Encode()

	request, err := http.NewRequest("GET", url.String(), nil)

	if username != "" || password != "" {
		request.SetBasicAuth(username, password)
	}

	return request, err
}

func isTokenDemand(resp *http.Response) *authService {
	if resp == nil {
		return nil
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return nil
	}
	return parseOauthHeader(resp)
}

func parseOauthHeader(resp *http.Response) *authService {
	challenges := parseAuthHeader(resp.Header)
	for _, challenge := range challenges {
		if challenge.Scheme == "bearer" {
			return &authService{
				Realm:   challenge.Parameters["realm"],
				Service: challenge.Parameters["service"],
				Scope:   challenge.Parameters["scope"],
			}
		}
	}
	return nil
}
