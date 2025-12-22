package relayer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type Transaction struct {
	To    string `json:"to"`
	Data  string `json:"data"`
	Value string `json:"value"`
}

type SignatureParams struct {
	GasPrice       string `json:"gasPrice,omitempty"`
	Operation      string `json:"operation,omitempty"`
	SafeTxnGas     string `json:"safeTxnGas,omitempty"`
	BaseGas        string `json:"baseGas,omitempty"`
	GasToken       string `json:"gasToken,omitempty"`
	RefundReceiver string `json:"refundReceiver,omitempty"`
}

type TransactionRequest struct {
	Type            string          `json:"type"`
	From            string          `json:"from"`
	To              string          `json:"to"`
	ProxyWallet     string          `json:"proxyWallet,omitempty"`
	Data            string          `json:"data"`
	Nonce           string          `json:"nonce,omitempty"`
	Signature       string          `json:"signature"`
	SignatureParams SignatureParams `json:"signatureParams"`
	Metadata        string          `json:"metadata,omitempty"`
}

type NoncePayload struct {
	Nonce string `json:"nonce"`
}

type GetDeployedResponse struct {
	Deployed bool `json:"deployed"`
}

type RelayerTransaction struct {
	TransactionID   string `json:"transactionID"`
	TransactionHash string `json:"transactionHash"`
	State           string `json:"state"`
}

type SubmitResponse struct {
	TransactionID   string `json:"transactionID"`
	State           string `json:"state"`
	TransactionHash string `json:"transactionHash"`
	Hash            string `json:"hash"`
}

type RelayClient struct {
	baseURL string
	chainID int

	signerKey *ecdsa.PrivateKey
	signer    common.Address

	builderCreds *BuilderCreds
	contractCfg  ContractConfig

	http *http.Client
}

func NewRelayClient(relayerURL string, chainID int, signerKey *ecdsa.PrivateKey, builderCreds *BuilderCreds) (*RelayClient, error) {
	if relayerURL == "" {
		return nil, errors.New("empty relayer url")
	}
	relayerURL = strings.TrimRight(relayerURL, "/")

	cfg, err := GetContractConfig(chainID)
	if err != nil {
		return nil, err
	}

	var signerAddr common.Address
	if signerKey != nil {
		signerAddr = crypto.PubkeyToAddress(signerKey.PublicKey)
	}

	return &RelayClient{
		baseURL:      relayerURL,
		chainID:      chainID,
		signerKey:    signerKey,
		signer:       signerAddr,
		builderCreds: builderCreds,
		contractCfg:  cfg,
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func (c *RelayClient) ExecuteSafe(ctx context.Context, txns []Transaction, metadata string) (SubmitResponse, error) {
	if c.signerKey == nil {
		return SubmitResponse{}, errors.New("signer unavailable")
	}
	if len(txns) == 0 {
		return SubmitResponse{}, errors.New("no transactions")
	}

	safeAddr, err := DeriveSafe(c.signer.Hex(), c.contractCfg.SafeContracts.SafeFactory)
	if err != nil {
		return SubmitResponse{}, err
	}

	deployed, err := c.GetDeployed(ctx, safeAddr)
	if err != nil {
		return SubmitResponse{}, err
	}
	if !deployed {
		return SubmitResponse{}, errors.New("SAFE_NOT_DEPLOYED")
	}

	noncePayload, err := c.GetNonce(ctx, c.signer.Hex(), "SAFE")
	if err != nil {
		return SubmitResponse{}, err
	}

	req, err := buildSafeRequest(c.chainID, c.signerKey, c.signer.Hex(), safeAddr, txns[0], noncePayload.Nonce, metadata)
	if err != nil {
		return SubmitResponse{}, err
	}

	return c.Submit(ctx, req)
}

func (c *RelayClient) GetNonce(ctx context.Context, address string, txType string) (NoncePayload, error) {
	q := url.Values{}
	q.Set("address", address)
	q.Set("type", txType)
	var out NoncePayload
	if err := c.get(ctx, "/nonce", q, &out); err != nil {
		return NoncePayload{}, err
	}
	return out, nil
}

func (c *RelayClient) GetDeployed(ctx context.Context, address string) (bool, error) {
	q := url.Values{}
	q.Set("address", address)
	var out GetDeployedResponse
	if err := c.get(ctx, "/deployed", q, &out); err != nil {
		return false, err
	}
	return out.Deployed, nil
}

func (c *RelayClient) GetTransaction(ctx context.Context, transactionID string) ([]RelayerTransaction, error) {
	q := url.Values{}
	q.Set("id", transactionID)
	var out []RelayerTransaction
	if err := c.get(ctx, "/transaction", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *RelayClient) Submit(ctx context.Context, req TransactionRequest) (SubmitResponse, error) {
	bodyBytes, _ := json.Marshal(req)
	body := string(bodyBytes)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/submit", bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")

	if c.builderCreds != nil && c.builderCreds.Key != "" && c.builderCreds.SecretB64 != "" && c.builderCreds.Passphrase != "" {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		sig, err := BuildBuilderSignature(c.builderCreds.SecretB64, ts, http.MethodPost, "/submit", body)
		if err != nil {
			return SubmitResponse{}, err
		}
		httpReq.Header.Set("POLY_BUILDER_API_KEY", c.builderCreds.Key)
		httpReq.Header.Set("POLY_BUILDER_PASSPHRASE", c.builderCreds.Passphrase)
		httpReq.Header.Set("POLY_BUILDER_SIGNATURE", sig)
		httpReq.Header.Set("POLY_BUILDER_TIMESTAMP", ts)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return SubmitResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SubmitResponse{}, fmt.Errorf("relayer submit http %d: %s", resp.StatusCode, string(raw))
	}

	var out SubmitResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return SubmitResponse{}, err
	}
	if out.Hash == "" && out.TransactionHash != "" {
		out.Hash = out.TransactionHash
	}
	return out, nil
}

// Wait mirrors ClientRelayerTransactionResponse.wait(): poll until MINED/CONFIRMED, fail on FAILED.
func (c *RelayClient) Wait(ctx context.Context, transactionID string) (*RelayerTransaction, error) {
	states := map[string]bool{
		"STATE_MINED":     true,
		"STATE_CONFIRMED": true,
	}
	fail := "STATE_FAILED"

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for polls := 0; polls < 100; polls++ {
		txns, err := c.GetTransaction(ctx, transactionID)
		if err == nil && len(txns) > 0 {
			t := txns[0]
			if t.State == fail {
				return nil, fmt.Errorf("relayer tx failed (onchain): %s", t.TransactionHash)
			}
			if states[t.State] {
				return &t, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
	return nil, errors.New("timeout waiting relayer tx")
}

func (c *RelayClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("relayer http %d: %s", resp.StatusCode, string(raw))
	}
	return json.Unmarshal(raw, out)
}
