package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/akeylesslabs/custom-producer/go/letsencrypt/internal/producer"
	"github.com/akeylesslabs/custom-producer/go/pkg/auth"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(ctx context.Context, r events.APIGatewayV2HTTPRequest) (interface{}, error) {
	log.Println("new request:", r.RawPath)

	p, err := producer.New(
		producer.WithDryRunEmail(os.Getenv("LE_DRY_RUN_EMAIL")),
		producer.WithDryRunDomain(os.Getenv("LE_DRY_RUN_DOMAIN")),
	)
	if err != nil {
		return nil, fmt.Errorf("can't setup producer: %w", err)
	}

	if err := auth.Authenticate(ctx, r.Headers["AkeylessCreds"], os.Getenv("AKEYLESS_ACCESS_ID")); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	switch strings.TrimSuffix(r.RawPath, "/") {
	case "/sync/create":
		var cr *producer.CreateRequest
		if err := json.Unmarshal([]byte(r.Body), &cr); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}

		return p.Create(cr)
	case "/sync/revoke":
		var rr *producer.RevokeRequest
		if err := json.Unmarshal([]byte(r.Body), &rr); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}

		return p.Revoke(rr)
	default:
		return nil, fmt.Errorf("invalid request path '%s'", r.RawPath)
	}
}

func main() {
	lambda.Start(handleRequest)
}
