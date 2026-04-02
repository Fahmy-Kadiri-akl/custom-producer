// Package webhook wraps Let's Encrypt custom producer with HTTP API. It
// exposes 2 endpoints: `/sync/create` and `/sync/revoke`. These endpoints
// implement Akeyless Custom Producer protocol.
package webhook

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/akeylesslabs/custom-producer/go/letsencrypt/internal/producer"
	"github.com/akeylesslabs/custom-producer/go/pkg/auth"
	"github.com/go-chi/chi/v5"
)

const credsHeader = "AkeylessCreds"

// New creates a new handler using the provided configuration. The returned
// handler can be used to serve Akeyless Custom Producer requests to generate
// Let's Encrypt certificates.
func New(p producer.Producer, opts ...Option) (http.Handler, error) {
	h := &hook{}

	for _, opt := range opts {
		opt(h)
	}

	r := chi.NewRouter()

	// it is very important to authenticate every request to prevent abuse
	r.Use(h.auth)

	// Akeyless custom producer must implement at least 2 endpoints:
	// create and revoke.
	r.Post("/sync/create", h.handle(h.create(p)))
	r.Post("/sync/revoke", h.handle(h.revoke(p)))

	return r, nil
}

type hook struct {
	accessID string
	itemName string
}

func (h *hook) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creds := r.Header.Get(credsHeader)
		if err := auth.Authenticate(r.Context(), creds, h.accessID, auth.WithAllowedItemName(h.itemName)); err == nil {
			log.Printf("producer '%s' authorized for item '%s'", h.accessID, h.itemName)
			next.ServeHTTP(w, r)
		} else {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
		}
	})
}

type wrapperFunc func(r *http.Request) (interface{}, error)

func (h *hook) create(p producer.Producer) wrapperFunc {
	return func(r *http.Request) (interface{}, error) {
		r.Body = http.MaxBytesReader(nil, r.Body, 1<<20) // 1MB max
		var cr *producer.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			return nil, newWebhookError("can't read request body", http.StatusBadRequest, err)
		}

		return p.Create(cr)
	}
}

func (h *hook) revoke(p producer.Producer) wrapperFunc {
	return func(r *http.Request) (interface{}, error) {
		r.Body = http.MaxBytesReader(nil, r.Body, 1<<20) // 1MB max
		var rr *producer.RevokeRequest
		if err := json.NewDecoder(r.Body).Decode(&rr); err != nil {
			return nil, newWebhookError("can't read request body", http.StatusBadRequest, err)
		}

		return p.Revoke(rr)
	}
}

func (h *hook) handle(f wrapperFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := f(r)
		if err != nil {
			log.Printf("%s request ended with error: %s\n", r.URL.String(), err.Error())

			if errors.Is(err, producer.ErrMissingSubClaim) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			var whErr *webhookError

			if errors.As(err, &whErr) {
				http.Error(w, whErr.Error(), whErr.code)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			log.Printf("failed to write response: %v", err)
		}
	}
}
