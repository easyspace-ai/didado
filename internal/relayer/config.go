package relayer

import "errors"

const (
	RelayerTxTypeSafe  = "SAFE"
	RelayerTxTypeProxy = "PROXY"
)

// From builder-relayer-client/src/constants
const (
	SafeInitCodeHash  = "0x2bce2127ff07fb632d16c8347c4ebf501f4841168bed00d9e6ef715ddb6fcecf"
	ProxyInitCodeHash = "0xd21df8dc65880a8606f09fe0ce3df9b8869287ab0b058be05aa9e8af6330a00b"
)

type ContractConfig struct {
	ProxyContracts ProxyContractConfig
	SafeContracts  SafeContractConfig
}

type ProxyContractConfig struct {
	RelayHub     string
	ProxyFactory string
}

type SafeContractConfig struct {
	SafeFactory   string
	SafeMultisend string
}

func GetContractConfig(chainID int) (ContractConfig, error) {
	switch chainID {
	case 137:
		return ContractConfig{
			ProxyContracts: ProxyContractConfig{
				ProxyFactory: "0xaB45c5A4B0c941a2F231C04C3f49182e1A254052",
				RelayHub:     "0xD216153c06E857cD7f72665E0aF1d7D82172F494",
			},
			SafeContracts: SafeContractConfig{
				SafeFactory:   "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b",
				SafeMultisend: "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761",
			},
		}, nil
	case 80002: // Amoy
		return ContractConfig{
			ProxyContracts: ProxyContractConfig{
				ProxyFactory: "",
				RelayHub:     "",
			},
			SafeContracts: SafeContractConfig{
				SafeFactory:   "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b",
				SafeMultisend: "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761",
			},
		}, nil
	default:
		return ContractConfig{}, errors.New("invalid network")
	}
}
