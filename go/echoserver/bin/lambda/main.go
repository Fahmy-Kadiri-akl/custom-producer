package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/akeylesslabs/custom-producer/go/echoserver/pkg/producer"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(ctx context.Context, r events.APIGatewayV2HTTPRequest) (interface{}, error) {
	// NOTE: request body may contain sensitive data such as secret payloads,
	// passwords, or tokens. Never log the request body.
	log.Println("new request:", r.RawPath)

	p := &producer.Producer{}

	// this dummy implementation doesn't require authentication because it
	// doesn't do anything. For actual production use, every request must be
	// authenticated!

	// creds := r.Headers["AkeylessCreds"]
	// allowedAccessID := "p-<some-id>"
	// if err := auth.Authenticate(ctx, creds, allowedAccessID); err != nil {
	// 	return nil, fmt.Errorf("unauthorized: %w", err)
	// }

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
	case "/sync/rotate":
		var rr *producer.RotateRequest
		if err := json.Unmarshal([]byte(r.Body), &rr); err != nil {
			return nil, fmt.Errorf("invalid request: %w", err)
		}

		return p.Rotate(rr)
	default:
		return nil, fmt.Errorf("invalid request path '%s'", r.RawPath)
	}
}

func main() {
	lambda.Start(handleRequest)
}
