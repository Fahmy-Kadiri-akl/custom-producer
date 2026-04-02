// Package auth allows to easily authenticate webhook requests with Akeyless
// Auth service.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const validationURL = "https://auth.akeyless.io/validate-producer-credentials"

var authClient = &http.Client{
	Timeout: 10 * time.Second,
}

// Authenticate validates that the provided credentials belong to the
// provided access ID, and optionally makes additional assertions.
//
// It uses Akeyless authentication service to confirm request initiator's
// identity.
func Authenticate(ctx context.Context, creds string, accessID string, opts ...Option) error {
	o := &options{}

	for _, opt := range opts {
		opt(o)
	}

	bs, err := json.Marshal(map[string]interface{}{
		"creds":              creds,
		"expected_access_id": accessID,
		"expected_item_name": o.itemName,
	})
	if err != nil {
		return fmt.Errorf("can't marshal validation request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, validationURL, bytes.NewReader(bs))
	if err != nil {
		return fmt.Errorf("can't create validation request: %w", err)
	}

	res, err := authClient.Do(req)
	if err != nil {
		return fmt.Errorf("validation request failed: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20)) // 1MB max
	if err != nil {
		return fmt.Errorf("can't read validation response body: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected validation response code %d: %s", res.StatusCode, string(body))
	}

	var reqParams map[string]interface{}
	if err := json.Unmarshal(body, &reqParams); err != nil {
		return fmt.Errorf("can't marshal validation response body '%s': %w", string(body), err)
	}

	if accessID != reqParams["access_id"] {
		return fmt.Errorf("mismatched access id")
	}

	return nil
}

// Option is an optional authentication assertion to be made.
type Option func(*options)

type options struct {
	itemName string
}

// WithAllowedItemName asserts that the incoming request is made by a producer
// with the provided name.
func WithAllowedItemName(name string) Option {
	return func(o *options) {
		o.itemName = name
	}
}
