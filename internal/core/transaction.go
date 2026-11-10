package core

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"tracey-sof/internal/model"
)

type decodedScriptAddressFunc func([]byte) (string, string, error)

func DecodeTransaction(rawHex string) (model.Transaction, error) {
	return decodeTransaction(rawHex, scriptAddress)
}

func decodeTransaction(rawHex string, decodeAddress decodedScriptAddressFunc) (model.Transaction, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(rawHex))
	if err != nil {
		return model.Transaction{}, fmt.Errorf("invalid transaction hex: %w", err)
	}
	var msg wire.MsgTx
	if err := msg.Deserialize(bytes.NewReader(raw)); err != nil {
		return model.Transaction{}, fmt.Errorf("decode transaction: %w", err)
	}
	tx := model.Transaction{TxID: msg.TxHash().String(), Hex: hex.EncodeToString(raw), Version: msg.Version, LockTime: msg.LockTime}
	for _, input := range msg.TxIn {
		coinbase := input.PreviousOutPoint.Hash.IsEqual(new(chainhash.Hash)) && input.PreviousOutPoint.Index == math.MaxUint32
		tx.Inputs = append(tx.Inputs, model.Input{PrevOut: model.Outpoint{TxID: input.PreviousOutPoint.Hash.String(), Vout: input.PreviousOutPoint.Index}, Coinbase: coinbase})
	}
	for i, output := range msg.TxOut {
		address, class, err := decodeAddress(output.PkScript)
		if err != nil {
			return model.Transaction{}, fmt.Errorf("decode output %d script: %w", i, err)
		}
		tx.Outputs = append(tx.Outputs, model.Output{Index: uint32(i), ValueSats: output.Value,
			ScriptPubKey: hex.EncodeToString(output.PkScript), ScriptType: class, Address: address})
	}
	return tx, nil
}

func ScriptAddress(rawHex string) (string, string, error) {
	script, err := hex.DecodeString(rawHex)
	if err != nil {
		return "", "", fmt.Errorf("invalid script hex: %w", err)
	}
	return scriptAddress(script)
}

type pkScriptAddressExtractor func([]byte, *chaincfg.Params) (txscript.ScriptClass, []btcutil.Address, int, error)

func scriptAddress(script []byte) (string, string, error) {
	return scriptAddressWith(script, txscript.ExtractPkScriptAddrs)
}

func scriptAddressWith(script []byte, extract pkScriptAddressExtractor) (string, string, error) {
	class, addresses, _, err := extract(script, &chaincfg.MainNetParams)
	if err != nil {
		return "", "", err
	}
	address := ""
	// P2PK and bare multisig can yield component public-key addresses, but the
	// output script itself does not have a unique conventional address.
	switch class {
	case txscript.PubKeyHashTy, txscript.ScriptHashTy, txscript.WitnessV0PubKeyHashTy,
		txscript.WitnessV0ScriptHashTy, txscript.WitnessV1TaprootTy:
		if len(addresses) == 1 {
			address = addresses[0].EncodeAddress()
		}
	}
	return address, class.String(), nil
}

// DecimalSats converts Core's BTC JSON number to satoshis without binary floats.
func DecimalSats(number string) (int64, error) {
	rat, ok := new(big.Rat).SetString(number)
	if !ok || rat.Sign() < 0 {
		return 0, fmt.Errorf("invalid nonnegative BTC amount %q", number)
	}
	rat.Mul(rat, big.NewRat(100000000, 1))
	if !rat.IsInt() {
		return 0, fmt.Errorf("BTC amount %q has sub-satoshi precision", number)
	}
	n := rat.Num()
	if !n.IsInt64() {
		return 0, fmt.Errorf("BTC amount %q is too large", number)
	}
	return n.Int64(), nil
}

func ParseHashHeight(best string, blocks int64) (model.Chain, error) {
	if len(best) != 64 || blocks < 0 {
		return model.Chain{}, fmt.Errorf("invalid chain tip")
	}
	if _, err := hex.DecodeString(best); err != nil {
		return model.Chain{}, fmt.Errorf("invalid best block hash")
	}
	return model.Chain{BestBlock: strings.ToLower(best), BestHeight: blocks}, nil
}

func SatString(sats int64) string {
	negative := sats < 0
	var n uint64
	if negative {
		n = uint64(-(sats + 1)) + 1
	} else {
		n = uint64(sats)
	}
	whole := strconv.FormatUint(n/100000000, 10)
	fraction := fmt.Sprintf("%08d", n%100000000)
	fraction = strings.TrimRight(fraction, "0")
	if fraction == "" {
		fraction = "0"
	}
	if negative {
		whole = "-" + whole
	}
	return whole + "." + fraction
}

func NetMatches(address string) bool {
	addr, err := btcutil.DecodeAddress(address, &chaincfg.MainNetParams)
	return err == nil && addr.IsForNet(&chaincfg.MainNetParams)
}
