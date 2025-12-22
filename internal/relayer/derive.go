package relayer

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func DeriveProxyWallet(address string, proxyFactory string) (string, error) {
	return deriveCreate2(address, proxyFactory, ProxyInitCodeHash, true)
}

func DeriveSafe(address string, safeFactory string) (string, error) {
	return deriveCreate2(address, safeFactory, SafeInitCodeHash, false)
}

// If packedSalt=true: salt=keccak256(encodePacked(["address"],[address])) i.e. 20-byte address
// else: salt=keccak256(abi.encode(address)) i.e. 32-byte left-padded address
func deriveCreate2(address string, factory string, initCodeHashHex string, packedSalt bool) (string, error) {
	addr := common.HexToAddress(address)
	f := common.HexToAddress(factory)

	initHash, err := decodeHex32(initCodeHashHex)
	if err != nil {
		return "", err
	}

	var saltInput []byte
	if packedSalt {
		saltInput = addr.Bytes() // 20 bytes
	} else {
		saltInput = common.LeftPadBytes(addr.Bytes(), 32)
	}
	salt := crypto.Keccak256Hash(saltInput)

	// create2: keccak256(0xff ++ factory ++ salt ++ init_code_hash)[12:]
	b := []byte{0xff}
	b = append(b, f.Bytes()...)
	b = append(b, salt.Bytes()...)
	b = append(b, initHash[:]...)
	h := crypto.Keccak256Hash(b)
	derived := common.BytesToAddress(h.Bytes()[12:])
	return derived.Hex(), nil
}

func decodeHex32(s string) ([32]byte, error) {
	var out [32]byte
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, err
	}
	if len(b) != 32 {
		return out, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return out, nil
}
