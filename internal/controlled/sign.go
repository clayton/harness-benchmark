package controlled

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Envelope struct {
	KeyID     string         `json:"key_id"`
	Payload   string         `json:"payload"`
	Signature string         `json:"signature"`
	Patch     string         `json:"patch,omitempty"`
	Report    map[string]any `json:"judge_report,omitempty"`
}

func Keygen(path string) (string, string, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(private)
	publicDER, _ := x509.MarshalPKIXPublicKey(public)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		return "", "", err
	}
	publicPath := path + ".pub.pem"
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644); err != nil {
		return "", "", err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(publicDER))[:16]
	return publicPath, fingerprint, nil
}

func Sign(keyPath, keyID string, payload any, patch string, report map[string]any) (Envelope, error) {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	var signedPayload map[string]any
	if err := json.Unmarshal(payloadRaw, &signedPayload); err != nil {
		return Envelope{}, fmt.Errorf("attestation payload must be a JSON object: %w", err)
	}
	reportRaw, err := canonicalAttestationJSON(report)
	if err != nil {
		return Envelope{}, err
	}
	signedPayload["artifacts"] = map[string]any{
		"patch_sha256":        sha256Hex([]byte(patch)),
		"judge_report_sha256": sha256Hex(reportRaw),
	}
	raw, err := canonicalAttestationJSON(signedPayload)
	if err != nil {
		return Envelope{}, err
	}
	keyRaw, err := os.ReadFile(keyPath)
	if err != nil {
		return Envelope{}, err
	}
	block, _ := pem.Decode(keyRaw)
	if block == nil {
		return Envelope{}, fmt.Errorf("invalid runner private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return Envelope{}, err
	}
	private, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return Envelope{}, fmt.Errorf("runner key is not Ed25519")
	}
	signature := ed25519.Sign(private, raw)
	return Envelope{KeyID: keyID, Payload: base64.StdEncoding.EncodeToString(raw), Signature: base64.StdEncoding.EncodeToString(signature), Patch: patch, Report: report}, nil
}

func canonicalAttestationJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func sha256Hex(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func Upload(baseURL string, envelope Envelope, client *http.Client) (map[string]any, error) {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	raw, _ := json.Marshal(envelope)
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/controlled_runs", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("rodeo %d: %s", response.StatusCode, body)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}
