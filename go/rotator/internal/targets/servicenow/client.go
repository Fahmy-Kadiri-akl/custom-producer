package servicenow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	instanceURL string
	adminUser   string
	adminPass   string
	httpClient  *http.Client
}

func NewClient(instanceURL, adminUser, adminPass string) *Client {
	return &Client{
		instanceURL: instanceURL, adminUser: adminUser, adminPass: adminPass,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// RotateClientSecret updates the client_secret on a ServiceNow OAuth application record.
func (c *Client) RotateClientSecret(ctx context.Context, appSysID, newSecret string) error {
	url := fmt.Sprintf("%s/api/now/table/oauth_entity/%s", c.instanceURL, appSysID)
	body, _ := json.Marshal(map[string]string{"client_secret": newSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.adminUser, c.adminPass)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update client secret (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GenerateSecret creates a cryptographically random hex string.
func GenerateSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
