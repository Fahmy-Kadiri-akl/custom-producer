package newrelic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const nerdGraphURL = "https://api.newrelic.com/graphql"

type Client struct {
	adminKey   string
	httpClient *http.Client
}

type KeyResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func NewClient(adminKey string) *Client {
	return &Client{adminKey: adminKey, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) query(ctx context.Context, gql string, variables map[string]interface{}) ([]byte, error) {
	payload, _ := json.Marshal(map[string]interface{}{"query": gql, "variables": variables})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nerdGraphURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Key", c.adminKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NerdGraph (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) CreateUserKey(ctx context.Context, accountID int, name string) (*KeyResponse, error) {
	gql := `mutation($keys: ApiAccessCreateInput!) { apiAccessCreateKeys(keys: $keys) { createdKeys { id key } errors { message } } }`
	vars := map[string]interface{}{
		"keys": map[string]interface{}{
			"userKeyCreate": []map[string]interface{}{
				{"accountId": accountID, "name": name},
			},
		},
	}
	body, err := c.query(ctx, gql, vars)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			APIAccessCreateKeys struct {
				CreatedKeys []KeyResponse `json:"createdKeys"`
				Errors      []struct{ Message string } `json:"errors"`
			} `json:"apiAccessCreateKeys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data.APIAccessCreateKeys.Errors) > 0 {
		return nil, fmt.Errorf("NerdGraph error: %s", resp.Data.APIAccessCreateKeys.Errors[0].Message)
	}
	if len(resp.Data.APIAccessCreateKeys.CreatedKeys) == 0 {
		return nil, fmt.Errorf("no key returned")
	}
	return &resp.Data.APIAccessCreateKeys.CreatedKeys[0], nil
}

func (c *Client) CreateIngestKey(ctx context.Context, accountID int, name, ingestType string) (*KeyResponse, error) {
	gql := `mutation($keys: ApiAccessCreateInput!) { apiAccessCreateKeys(keys: $keys) { createdKeys { id key } errors { message } } }`
	vars := map[string]interface{}{
		"keys": map[string]interface{}{
			"ingestKeyCreate": []map[string]interface{}{
				{"accountId": accountID, "name": name, "ingestType": ingestType},
			},
		},
	}
	body, err := c.query(ctx, gql, vars)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			APIAccessCreateKeys struct {
				CreatedKeys []KeyResponse `json:"createdKeys"`
				Errors      []struct{ Message string } `json:"errors"`
			} `json:"apiAccessCreateKeys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data.APIAccessCreateKeys.Errors) > 0 {
		return nil, fmt.Errorf("NerdGraph error: %s", resp.Data.APIAccessCreateKeys.Errors[0].Message)
	}
	if len(resp.Data.APIAccessCreateKeys.CreatedKeys) == 0 {
		return nil, fmt.Errorf("no key returned")
	}
	return &resp.Data.APIAccessCreateKeys.CreatedKeys[0], nil
}

func (c *Client) DeleteKey(ctx context.Context, keyID string) error {
	gql := `mutation($keys: ApiAccessDeleteInput!) { apiAccessDeleteKeys(keys: $keys) { deletedKeys { id } errors { message } } }`
	vars := map[string]interface{}{
		"keys": map[string]interface{}{
			"keyIds": []string{keyID},
		},
	}
	body, err := c.query(ctx, gql, vars)
	if err != nil {
		return err
	}
	var resp struct {
		Data struct {
			APIAccessDeleteKeys struct {
				Errors []struct{ Message string } `json:"errors"`
			} `json:"apiAccessDeleteKeys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}
	if len(resp.Data.APIAccessDeleteKeys.Errors) > 0 {
		return fmt.Errorf("NerdGraph error: %s", resp.Data.APIAccessDeleteKeys.Errors[0].Message)
	}
	return nil
}
