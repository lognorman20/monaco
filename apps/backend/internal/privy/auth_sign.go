package privy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const authorizationSignatureHeader = "privy-authorization-signature"

type authorizationSignaturePayload struct {
	Version int               `json:"version"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Body    map[string]any    `json:"body"`
	Headers map[string]string `json:"headers"`
}

func authorizationSignatureURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func bodyMapForAuthorization(body []byte) (map[string]any, error) {
	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return nil, fmt.Errorf("%w: invalid authorization body: %v", ErrAPI, err)
	}
	return bodyMap, nil
}

func authorizationHeaders(appID, idempotencyKey string) map[string]string {
	headers := map[string]string{
		"privy-app-id": appID,
	}
	if idempotencyKey != "" {
		headers["privy-idempotency-key"] = idempotencyKey
	}
	return headers
}

func buildAuthorizationSignaturePayload(method, url string, body map[string]any, headers map[string]string) authorizationSignaturePayload {
	return authorizationSignaturePayload{
		Version: 1,
		Method:  method,
		URL:     url,
		Body:    body,
		Headers: headers,
	}
}

func verifyAuthorizationSignature(privyAuthorizationKey, signature string, payload authorizationSignaturePayload) (bool, error) {
	serialized, err := canonicalizeJSON(payload)
	if err != nil {
		return false, err
	}

	privateKey, err := parseAuthorizationPrivateKey(privyAuthorizationKey)
	if err != nil {
		return false, err
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}

	hash := sha256.Sum256([]byte(serialized))
	return ecdsa.VerifyASN1(&privateKey.PublicKey, hash[:], signatureBytes), nil
}

func signAuthorizationPayload(privyAuthorizationKey string, payload authorizationSignaturePayload) (string, error) {
	serialized, err := canonicalizeJSON(payload)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize authorization payload: %v", ErrAPI, err)
	}

	privateKey, err := parseAuthorizationPrivateKey(privyAuthorizationKey)
	if err != nil {
		return "", fmt.Errorf("%w: parse authorization private key: %v", ErrAPI, err)
	}

	hash := sha256.Sum256([]byte(serialized))
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("%w: sign authorization payload: %v", ErrAPI, err)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func parseAuthorizationPrivateKey(privyAuthorizationKey string) (*ecdsa.PrivateKey, error) {
	pkcs8B64 := strings.TrimPrefix(strings.TrimSpace(privyAuthorizationKey), "wallet-auth:")
	pkcs8Bytes, err := base64.StdEncoding.DecodeString(pkcs8B64)
	if err != nil {
		return nil, err
	}

	key, err := x509.ParsePKCS8PrivateKey(pkcs8Bytes)
	if err != nil {
		return nil, err
	}

	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("authorization key is not an ECDSA private key")
	}
	return ecdsaKey, nil
}

func canonicalizeJSON(value any) (string, error) {
	switch v := value.(type) {
	case authorizationSignaturePayload:
		return canonicalizeValue(map[string]any{
			"version": v.Version,
			"method":  v.Method,
			"url":     v.URL,
			"body":    v.Body,
			"headers": v.Headers,
		})
	default:
		return canonicalizeValue(value)
	}
}

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
	case map[string]string:
		asAny := make(map[string]any, len(v))
		for key, val := range v {
			asAny[key] = val
		}
		return canonicalizeValue(asAny)
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
