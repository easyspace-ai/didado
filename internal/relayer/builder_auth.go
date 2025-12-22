package relayer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

type BuilderCreds struct {
	Key        string
	SecretB64  string
	Passphrase string
}

type BuilderHeaders struct {
	POLY_BUILDER_API_KEY    string `json:"POLY_BUILDER_API_KEY"`
	POLY_BUILDER_PASSPHRASE string `json:"POLY_BUILDER_PASSPHRASE"`
	POLY_BUILDER_SIGNATURE  string `json:"POLY_BUILDER_SIGNATURE"`
	POLY_BUILDER_TIMESTAMP  string `json:"POLY_BUILDER_TIMESTAMP"`
}

// BuildBuilderSignature mirrors builder-signing-sdk:
// sig = base64(hmac_sha256(base64_decode(secret), timestamp+method+path+body))
// then convert + -> -, / -> _, keep "=" padding.
func BuildBuilderSignature(secretB64 string, timestamp string, method string, path string, body string) (string, error) {
	secretB64 = strings.TrimSpace(secretB64)

	// builder-signing uses standard base64, but accept url-safe too.
	key, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil {
		key, err = base64.URLEncoding.DecodeString(secretB64)
		if err != nil {
			return "", err
		}
	}

	msg := timestamp + method + path + body
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msg))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// url safe: '+'->'-', '/'->'_' (keep '=')
	sig = strings.ReplaceAll(sig, "+", "-")
	sig = strings.ReplaceAll(sig, "/", "_")
	return sig, nil
}
