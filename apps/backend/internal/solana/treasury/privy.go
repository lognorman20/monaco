package treasury

// Privy server-wallet calls, ported from archive/main-before-dynamic
// apps/backend/internal/privy/{http,auth_sign,treasury}.go.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const authorizationSignatureHeader = "privy-authorization-signature"

type createWalletRequest struct {
	ChainType   string `json:"chain_type"`
	DisplayName string `json:"display_name,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
}

type createWalletResponse struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type signTransactionRPCRequest struct {
	Method string          `json:"method"`
	Params walletRPCParams `json:"params"`
}

type walletRPCParams struct {
	Transaction string `json:"transaction"`
	Encoding    string `json:"encoding"`
}

type walletRPCResponse struct {
	Data struct {
		SignedTransaction string `json:"signed_transaction"`
	} `json:"data"`
}

// EnsureTreasury creates the group's Solana treasury wallet in Privy. The idempotency key and
// external id are the group id, so a retry after a lost response returns the same wallet.
func (c *HTTPClient) EnsureTreasury(ctx context.Context, groupID string) (TreasuryRef, error) {
	if groupID == "" {
		return TreasuryRef{}, fmt.Errorf("%w: missing group id", ErrAPI)
	}
	payload, err := json.Marshal(createWalletRequest{
		ChainType:   "solana",
		DisplayName: "monaco-treasury",
		ExternalID:  groupID,
	})
	if err != nil {
		return TreasuryRef{}, err
	}
	respBody, status, err := c.doPrivyRequest(ctx, http.MethodPost, "/v1/wallets", payload, "treasury-"+groupID, false)
	if err != nil {
		return TreasuryRef{}, err
	}
	if status < 200 || status >= 300 {
		return TreasuryRef{}, fmt.Errorf("%w: create wallet status %d: %s", ErrAPI, status, string(respBody))
	}
	var wallet createWalletResponse
	if err := json.Unmarshal(respBody, &wallet); err != nil {
		return TreasuryRef{}, err
	}
	if wallet.ID == "" || wallet.Address == "" {
		return TreasuryRef{}, fmt.Errorf("%w: create wallet missing id or address", ErrAPI)
	}
	return TreasuryRef{GroupID: groupID, PrivyWalletID: wallet.ID, SolanaAddress: wallet.Address}, nil
}

func (c *HTTPClient) signSolanaTransaction(ctx context.Context, walletID, txBase64 string) (string, error) {
	payload, err := json.Marshal(signTransactionRPCRequest{
		Method: "signTransaction",
		Params: walletRPCParams{Transaction: txBase64, Encoding: "base64"},
	})
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/v1/wallets/%s/rpc", url.PathEscape(walletID))
	respBody, status, err := c.doPrivyRequest(ctx, http.MethodPost, path, payload, "", true)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("%w: sign transaction status %d: %s", ErrAPI, status, string(respBody))
	}
	var rpcResp walletRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", err
	}
	if rpcResp.Data.SignedTransaction == "" {
		return "", fmt.Errorf("%w: sign transaction missing signed_transaction", ErrAPI)
	}
	return rpcResp.Data.SignedTransaction, nil
}

func (c *HTTPClient) doPrivyRequest(ctx context.Context, method, path string, body []byte, idempotencyKey string, requireAuthorization bool) ([]byte, int, error) {
	if requireAuthorization && c.cfg.PrivyAuthorizationPrivateKey == "" {
		return nil, 0, fmt.Errorf("%w: PRIVY_AUTHORIZATION_PRIVATE_KEY is required for wallet rpc", ErrAPI)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.privyURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("privy-app-id", c.cfg.PrivyAppID)
	if idempotencyKey != "" {
		req.Header.Set("privy-idempotency-key", idempotencyKey)
	}
	if requireAuthorization {
		var bodyMap map[string]any
		if err := json.Unmarshal(body, &bodyMap); err != nil {
			return nil, 0, fmt.Errorf("%w: invalid authorization body: %v", ErrAPI, err)
		}
		headers := map[string]any{"privy-app-id": c.cfg.PrivyAppID}
		if idempotencyKey != "" {
			headers["privy-idempotency-key"] = idempotencyKey
		}
		signature, err := signAuthorizationPayload(c.cfg.PrivyAuthorizationPrivateKey, map[string]any{
			"version": 1,
			"method":  method,
			"url":     strings.TrimRight(c.privyURL, "/") + path,
			"body":    bodyMap,
			"headers": headers,
		})
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set(authorizationSignatureHeader, signature)
	}
	req.SetBasicAuth(c.cfg.PrivyAppID, c.cfg.PrivyAppSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Warn("privy solana treasury request failed", "method", method, "path", path, "err", err)
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	slog.Info("privy solana treasury response", "method", method, "path", path, "status", resp.StatusCode)
	return respBody, resp.StatusCode, nil
}

// signAuthorizationPayload signs Privy's canonical JSON authorization payload with the app's
// P-256 authorization key ("wallet-auth:" + base64 PKCS#8).
func signAuthorizationPayload(privyAuthorizationKey string, payload map[string]any) (string, error) {
	serialized, err := canonicalizeValue(payload)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize authorization payload: %v", ErrAPI, err)
	}
	pkcs8B64 := strings.TrimPrefix(strings.TrimSpace(privyAuthorizationKey), "wallet-auth:")
	pkcs8Bytes, err := base64.StdEncoding.DecodeString(pkcs8B64)
	if err != nil {
		return "", fmt.Errorf("%w: parse authorization private key: %v", ErrAPI, err)
	}
	key, err := x509.ParsePKCS8PrivateKey(pkcs8Bytes)
	if err != nil {
		return "", fmt.Errorf("%w: parse authorization private key: %v", ErrAPI, err)
	}
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("%w: authorization key is not an ECDSA private key", ErrAPI)
	}
	hash := sha256.Sum256([]byte(serialized))
	signature, err := ecdsa.SignASN1(rand.Reader, ecdsaKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("%w: sign authorization payload: %v", ErrAPI, err)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// canonicalizeValue is RFC 8785-style JSON: object keys sorted, no insignificant whitespace.
func canonicalizeValue(value any) (string, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyJSON, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			buf.Write(keyJSON)
			buf.WriteByte(':')
			part, err := canonicalizeValue(v[key])
			if err != nil {
				return "", err
			}
			buf.WriteString(part)
		}
		buf.WriteByte('}')
		return buf.String(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			part, err := canonicalizeValue(item)
			if err != nil {
				return "", err
			}
			buf.WriteString(part)
		}
		buf.WriteByte(']')
		return buf.String(), nil
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}
