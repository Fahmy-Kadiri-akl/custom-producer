package apigee

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
)

// saKeyFile mirrors the fields of a real GCP service-account key file.
type saKeyFile struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// testSAJSON builds a service-account key file whose private key is a freshly
// generated RSA key, with token_uri pointed at the caller-supplied endpoint.
// It returns the JSON string and the matching public key so callers can
// verify assertion signatures.
func testSAJSON(t *testing.T, tokenURI string) (string, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	doc := saKeyFile{
		Type:         "service_account",
		ProjectID:    "example-project",
		PrivateKeyID: "kid123",
		PrivateKey:   string(pemBlock),
		ClientEmail:  "apigee-rotator@example-project.iam.gserviceaccount.com",
		TokenURI:     tokenURI,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal sa key file: %v", err)
	}
	return string(b), &key.PublicKey
}

func mustUnmarshal(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestType(t *testing.T) {
	if got := New().Type(); got != "apigee_x_token" {
		t.Fatalf("Type() = %q, want apigee_x_token", got)
	}
}

func TestEffectiveScope(t *testing.T) {
	if got := effectiveScope(""); got != defaultScope {
		t.Fatalf("effectiveScope(\"\") = %q, want %q", got, defaultScope)
	}
	if got := effectiveScope("https://api/x"); got != "https://api/x" {
		t.Fatalf("effectiveScope set value = %q", got)
	}
}

func TestResolveServiceAccount(t *testing.T) {
	t.Run("default env var", func(t *testing.T) {
		t.Setenv(defaultSAEnvVar, `{"client_email":"x"}`)
		got, err := resolveServiceAccount("")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != `{"client_email":"x"}` {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("custom ref", func(t *testing.T) {
		t.Setenv("APIGEE_SA_TWO", `{"client_email":"y"}`)
		got, err := resolveServiceAccount("APIGEE_SA_TWO")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != `{"client_email":"y"}` {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("unset", func(t *testing.T) {
		t.Setenv(defaultSAEnvVar, "")
		_, err := resolveServiceAccount("")
		if err == nil || !strings.Contains(err.Error(), defaultSAEnvVar) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestParseRSAPrivateKey(t *testing.T) {
	t.Run("PKCS8", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		pemStr := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		parsed, err := parseRSAPrivateKey(string(pemStr))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if parsed.N.Cmp(key.N) != 0 {
			t.Fatal("parsed modulus mismatch")
		}
	})
	t.Run("PKCS1", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		pemStr := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		parsed, err := parseRSAPrivateKey(string(pemStr))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if parsed.N.Cmp(key.N) != 0 {
			t.Fatal("parsed modulus mismatch")
		}
	})
	t.Run("invalid", func(t *testing.T) {
		if _, err := parseRSAPrivateKey("not a pem"); err == nil {
			t.Fatal("want error for non-PEM input")
		}
	})
}

func TestBuildAssertion(t *testing.T) {
	const aud = "https://oauth2.googleapis.com/token"
	saJSON, pub := testSAJSON(t, aud)
	var key saKey
	mustUnmarshal(t, []byte(saJSON), &key)

	assertion, err := buildAssertion(key, "sc1 sc2", aud)
	if err != nil {
		t.Fatalf("buildAssertion: %v", err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("want 3 JWT segments, got %d", len(parts))
	}

	var header map[string]string
	mustUnmarshal(t, decodeB64(t, parts[0]), &header)
	if header["alg"] != "RS256" {
		t.Fatalf("alg = %q", header["alg"])
	}
	if header["typ"] != "JWT" {
		t.Fatalf("typ = %q", header["typ"])
	}
	if header["kid"] != "kid123" {
		t.Fatalf("kid = %q", header["kid"])
	}

	var claims map[string]any
	mustUnmarshal(t, decodeB64(t, parts[1]), &claims)
	if claims["iss"] != "apigee-rotator@example-project.iam.gserviceaccount.com" {
		t.Fatalf("iss = %v", claims["iss"])
	}
	if claims["scope"] != "sc1 sc2" {
		t.Fatalf("scope = %v", claims["scope"])
	}
	if claims["aud"] != aud {
		t.Fatalf("aud = %v", claims["aud"])
	}
	if claims["jti"] == nil || claims["jti"] == "" {
		t.Fatal("missing jti")
	}
	exp, _ := claims["exp"].(float64)
	iat, _ := claims["iat"].(float64)
	if int(exp-iat) != 3600 {
		t.Fatalf("exp-iat = %v, want 3600", exp-iat)
	}

	// Signature must verify against the public key.
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig := decodeB64(t, parts[2])
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func decodeB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode %q: %v", s, err)
	}
	return b
}

func TestMintAccessToken_OK(t *testing.T) {
	var sawAssertion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if gt := r.PostForm.Get("grant_type"); gt != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", gt)
		}
		sawAssertion = r.PostForm.Get("assertion")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.fake","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	saJSON, _ := testSAJSON(t, srv.URL)
	tok, exp, err := mintAccessToken(context.Background(), saJSON, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		t.Fatalf("mintAccessToken: %v", err)
	}
	if tok != "ya29.fake" {
		t.Fatalf("token = %q", tok)
	}
	if exp != 3600 {
		t.Fatalf("expires_in = %d", exp)
	}
	if sawAssertion == "" || len(strings.Split(sawAssertion, ".")) != 3 {
		t.Fatalf("server saw bad assertion: %q", sawAssertion)
	}
}

func TestMintAccessToken_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	saJSON, _ := testSAJSON(t, srv.URL)
	_, _, err := mintAccessToken(context.Background(), saJSON, "sc")
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("err = %v", err)
	}
}

func TestMintAccessToken_MissingKey(t *testing.T) {
	_, _, err := mintAccessToken(context.Background(), `{"client_email":"x"}`, "sc")
	if err == nil || !strings.Contains(err.Error(), "missing private_key") {
		t.Fatalf("err = %v", err)
	}
}

func TestRotate_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.rotated","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	saJSON, _ := testSAJSON(t, srv.URL)
	t.Setenv(defaultSAEnvVar, saJSON)

	// Payload carries no service-account key; only type and org context.
	payloadBytes, err := json.Marshal(map[string]any{
		"type":         "apigee_x_token",
		"organization": "example-project",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	resp, err := New().Rotate(context.Background(), &types.RotateRequest{Payload: string(payloadBytes)})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	var out ApigeeTokenPayload
	mustUnmarshal(t, []byte(resp.Payload), &out)
	if out.Token != "ya29.rotated" {
		t.Fatalf("token = %q", out.Token)
	}
	if out.ExpiresAt == "" {
		t.Fatal("expires_at empty")
	}
	if out.Organization != "example-project" {
		t.Fatalf("organization not preserved: %q", out.Organization)
	}

	// Clean-output guarantee: the round-tripped payload that consumers read
	// must never contain the service-account key.
	if strings.Contains(resp.Payload, "private_key") {
		t.Fatal("response payload contains private_key")
	}
	if strings.Contains(resp.Payload, "service_account") {
		t.Fatal("response payload contains service_account")
	}
}

func TestRotate_CustomRef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.two","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	saJSON, _ := testSAJSON(t, srv.URL)
	t.Setenv("APIGEE_SA_TWO", saJSON)

	payload, _ := json.Marshal(map[string]any{
		"type":                "apigee_x_token",
		"service_account_ref": "APIGEE_SA_TWO",
	})
	resp, err := New().Rotate(context.Background(), &types.RotateRequest{Payload: string(payload)})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	var out ApigeeTokenPayload
	mustUnmarshal(t, []byte(resp.Payload), &out)
	if out.Token != "ya29.two" {
		t.Fatalf("token = %q", out.Token)
	}
	if strings.Contains(resp.Payload, "private_key") {
		t.Fatal("response payload contains private_key")
	}
}

func TestRotate_MissingEnvError(t *testing.T) {
	t.Setenv(defaultSAEnvVar, "")
	_, err := New().Rotate(context.Background(), &types.RotateRequest{
		Payload: `{"type":"apigee_x_token","organization":"o"}`,
	})
	if err == nil || !strings.Contains(err.Error(), defaultSAEnvVar) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateAndID(t *testing.T) {
	payloadBytes, _ := json.Marshal(map[string]any{
		"type":         "apigee_x_token",
		"organization": "example-project",
		"token":        "existing",
	})

	resp, err := New().Create(context.Background(), &types.CreateRequest{Payload: string(payloadBytes)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID != "apigee-x-example-project" {
		t.Fatalf("ID = %q, want apigee-x-example-project", resp.ID)
	}
	if !strings.Contains(fmt.Sprint(resp.Response), "existing") {
		t.Fatalf("Response did not echo current token: %v", resp.Response)
	}
}

func TestRevoke(t *testing.T) {
	resp, err := New().Revoke(context.Background(), &types.RevokeRequest{IDs: []string{"id-1"}})
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if len(resp.Revoked) != 1 || resp.Revoked[0] != "id-1" {
		t.Fatalf("Revoked = %v", resp.Revoked)
	}
}
