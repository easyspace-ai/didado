package relayer

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

func buildSafeRequest(chainID int, key *ecdsa.PrivateKey, from string, safe string, txn Transaction, nonce string, metadata string) (TransactionRequest, error) {
	if txn.Value == "" {
		txn.Value = "0"
	}
	if metadata == "" {
		metadata = ""
	}

	// Build EIP-712 typed data hash (viem hashTypedData)
	structHash, err := safeTypedDataHash(chainID, safe, txn.To, txn.Value, txn.Data, 0, "0", "0", "0", zeroAddress(), zeroAddress(), nonce)
	if err != nil {
		return TransactionRequest{}, err
	}

	// signMessage over structHash (ethers/viem behavior)
	sig, err := signMessage(key, structHash)
	if err != nil {
		return TransactionRequest{}, err
	}
	packedSig, err := splitAndPackSig(sig)
	if err != nil {
		return TransactionRequest{}, err
	}

	return TransactionRequest{
		Type:        "SAFE",
		From:        from,
		To:          txn.To,
		ProxyWallet: safe,
		Data:        txn.Data,
		Nonce:       nonce,
		Signature:   packedSig,
		SignatureParams: SignatureParams{
			GasPrice:       "0",
			Operation:      "0",
			SafeTxnGas:     "0",
			BaseGas:        "0",
			GasToken:       zeroAddress(),
			RefundReceiver: zeroAddress(),
		},
		Metadata: metadata,
	}, nil
}

func safeTypedDataHash(
	chainID int,
	safe string,
	to string,
	value string,
	data string,
	operation int,
	safeTxGas string,
	baseGas string,
	gasPrice string,
	gasToken string,
	refundReceiver string,
	nonce string,
) ([]byte, error) {
	// apitypes wants domain + types + message
	td := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"SafeTx": []apitypes.Type{
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "data", Type: "bytes"},
				{Name: "operation", Type: "uint8"},
				{Name: "safeTxGas", Type: "uint256"},
				{Name: "baseGas", Type: "uint256"},
				{Name: "gasPrice", Type: "uint256"},
				{Name: "gasToken", Type: "address"},
				{Name: "refundReceiver", Type: "address"},
				{Name: "nonce", Type: "uint256"},
			},
		},
		PrimaryType: "SafeTx",
		Domain: apitypes.TypedDataDomain{
			ChainId:           math.NewHexOrDecimal256(int64(chainID)),
			VerifyingContract: safe,
		},
		Message: apitypes.TypedDataMessage{
			"to":             to,
			"value":          value,
			"data":           data,
			"operation":      operation,
			"safeTxGas":      safeTxGas,
			"baseGas":        baseGas,
			"gasPrice":       gasPrice,
			"gasToken":       gasToken,
			"refundReceiver": refundReceiver,
			"nonce":          nonce,
		},
	}

	domainHash, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return nil, err
	}
	msgHash, err := td.HashStruct(td.PrimaryType, td.Message)
	if err != nil {
		return nil, err
	}
	// EIP712 digest
	raw := []byte{0x19, 0x01}
	raw = append(raw, domainHash...)
	raw = append(raw, msgHash...)
	digest := crypto.Keccak256(raw)
	return digest, nil
}

func signMessage(key *ecdsa.PrivateKey, msg32 []byte) ([]byte, error) {
	if len(msg32) != 32 {
		return nil, errors.New("expected 32-byte message")
	}
	hash := accounts.TextHash(msg32)
	return crypto.Sign(hash, key)
}

func splitAndPackSig(sig []byte) (string, error) {
	if len(sig) != 65 {
		return "", errors.New("invalid signature length")
	}
	r := sig[:32]
	s := sig[32:64]
	v := sig[64]

	switch v {
	case 0, 1:
		v += 31
	case 27, 28:
		v += 4
	default:
		return "", errors.New("invalid signature v")
	}

	packed := make([]byte, 0, 65)
	packed = append(packed, r...)
	packed = append(packed, s...)
	packed = append(packed, v)
	return "0x" + hex.EncodeToString(packed), nil
}

func zeroAddress() string { return common.Address{}.Hex() }
