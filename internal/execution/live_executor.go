package execution

import (
	"log"
	"math/big"
	"polymarket-go/internal/polymarket"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type LiveExecutor struct {
	Client        *polymarket.ClobClient
	FunderAddress common.Address
	EthClient     *ethclient.Client
	ChainID       int64
}

func NewLiveExecutor(client *polymarket.ClobClient, funderAddress string, rpcURL string) (*LiveExecutor, error) {
	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, err
	}

	return &LiveExecutor{
		Client:        client,
		FunderAddress: common.HexToAddress(funderAddress),
		EthClient:     ethClient,
		ChainID:       137,
	}, nil
}

func (e *LiveExecutor) GetBalance() (float64, error) {
	// Need USDC contract call
	// For now return dummy or implement later
	return 0, nil
}

func (e *LiveExecutor) PlaceOrder(params TradeParams) (string, error) {
	log.Printf("📝 [LIVE] Placing Order: %s %.2f @ $%.2f", params.Side, params.Size, params.Price)
	
	orderID, err := e.Client.CreateOrder(params.TokenID, params.Price, params.Side, params.Size)
	if err != nil {
		return "", err
	}
	return orderID, nil
}

func (e *LiveExecutor) CancelAll() error {
	return e.Client.CancelAll()
}

func (e *LiveExecutor) CancelOrder(orderID string) error {
	return e.Client.CancelOrder(orderID)
}

func (e *LiveExecutor) GetOrderStatus(orderID string) (float64, bool, float64, error) {
	// Calls client to get order
	return 0, false, 0, nil
}

func (e *LiveExecutor) GetPositions(tokenIDYes, tokenIDNo string) (float64, float64, error) {
	// Call CTF contract
	return 0, 0, nil
}

func (e *LiveExecutor) GetOpenOrders(marketSlug string) ([]OrderInfo, error) {
	return nil, nil
}

func (e *LiveExecutor) GetAddress() string {
	return e.FunderAddress.Hex()
}

func (e *LiveExecutor) RedeemPositions(conditionID string) (string, error) {
	// Call CTF contract redeem
	log.Printf("🔗 [LIVE] Submitting Redemption for %s...", conditionID)
	// Would involve building tx, signing, sending
	return "0xhash", nil
}

// Helper to convert float to big.Int with decimals
func toWei(amount float64, decimals int) *big.Int {
	// ... implementation
	return big.NewInt(0)
}
