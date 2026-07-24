// Package handler provides the HTTP layer for the unified custom producer.
// It exposes /sync/create, /sync/revoke, and /sync/rotate endpoints with
// type-based dispatch to registered rotation targets.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/akeylesslabs/custom-producer/go/pkg/auth"
	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/akeylesslabs/custom-producer/go/rotator/internal/registry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

const credsHeader = "AkeylessCreds"

// Config holds handler configuration.
type Config struct {
	AccessID string
	ItemName string
	SkipAuth bool
}

// New creates an http.Handler with all routes wired to the given registry.
func New(reg *registry.Registry, cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	r.Route("/sync", func(r chi.Router) {
		if !cfg.SkipAuth {
			r.Use(akeylessAuth(cfg.AccessID, cfg.ItemName))
		} else {
			log.Warn().Msg("SKIP_AUTH=true — authentication disabled")
		}
		r.Post("/create", handleCreate(reg))
		r.Post("/revoke", handleRevoke(reg))
		r.Post("/rotate", handleRotate(reg))
	})

	r.Post("/webhook/rotation-event", handleWebhook())

	return r
}

func akeylessAuth(accessID, itemName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			creds := r.Header.Get(credsHeader)
			if creds == "" {
				jsonError(w, "missing AkeylessCreds header", http.StatusUnauthorized)
				return
			}
			if accessID == "" {
				log.Warn().Msg("AKEYLESS_ACCESS_ID not set, skipping validation")
				next.ServeHTTP(w, r)
				return
			}
			opts := []auth.Option{}
			if itemName != "" {
				opts = append(opts, auth.WithAllowedItemName(itemName))
			}
			if err := auth.Authenticate(r.Context(), creds, accessID, opts...); err != nil {
				log.Error().Err(err).Msg("authentication failed")
				jsonError(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractType parses a JSON payload string and returns the "type" field.
func extractType(payload string) (string, error) {
	var meta struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(payload), &meta); err != nil {
		return "", fmt.Errorf("parse payload for type field: %w", err)
	}
	if meta.Type == "" {
		return "", fmt.Errorf("payload missing required \"type\" field")
	}
	return meta.Type, nil
}

func handleCreate(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CreateRequest
		if err := decodeBody(r, &req); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		typeName, err := extractType(req.Payload)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		target, ok := reg.Get(typeName)
		if !ok {
			jsonError(w, fmt.Sprintf("unknown target type: %q (registered: %v)", typeName, reg.Types()), http.StatusBadRequest)
			return
		}
		log.Info().Str("type", typeName).Str("endpoint", "create").Msg("dispatching request")
		resp, err := target.Create(r.Context(), &req)
		if err != nil {
			log.Error().Err(err).Str("type", typeName).Msg("create failed")
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, resp)
	}
}

func handleRevoke(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RevokeRequest
		if err := decodeBody(r, &req); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		typeName, err := extractType(req.Payload)
		if err != nil {
			// For revoke, payload may be empty — just acknowledge
			jsonOK(w, &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"})
			return
		}
		target, ok := reg.Get(typeName)
		if !ok {
			jsonOK(w, &types.RevokeResponse{Revoked: req.IDs, Message: "unknown type, acknowledged"})
			return
		}
		log.Info().Str("type", typeName).Strs("ids", req.IDs).Msg("dispatching revoke")
		resp, err := target.Revoke(r.Context(), &req)
		if err != nil {
			log.Error().Err(err).Str("type", typeName).Msg("revoke failed")
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, resp)
	}
}

func handleRotate(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.RotateRequest
		if err := decodeBody(r, &req); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		typeName, err := extractType(req.Payload)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		target, ok := reg.Get(typeName)
		if !ok {
			jsonError(w, fmt.Sprintf("unknown target type: %q (registered: %v)", typeName, reg.Types()), http.StatusBadRequest)
			return
		}
		log.Info().Str("type", typeName).Str("endpoint", "rotate").Msg("dispatching request")
		resp, err := target.Rotate(r.Context(), &req)
		if err != nil {
			log.Error().Err(err).Str("type", typeName).Msg("rotate failed")
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, resp)
	}
}

func handleWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event map[string]interface{}
		if err := json.Unmarshal(body, &event); err != nil {
			log.Warn().Str("raw", string(body)).Msg("received non-JSON webhook")
		} else {
			log.Info().Interface("event", event).Msg("received rotation event")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"received"}`))
	}
}

func decodeBody(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
