package probe

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestAnalyzeCoinbaseFindsWorkerOutputs(t *testing.T) {
	workerScript, _ := hex.DecodeString("76a914111111111111111111111111111111111111111188ac")
	raw, _ := hex.DecodeString("0100000001" + zeroHex(32) + "ffffffff0100ffffffff01" + "00f2052a01000000" + "19" + hex.EncodeToString(workerScript) + "00000000")
	summary, err := analyzeCoinbase(raw, workerScript)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.WorkerWalletSeen || summary.WorkerSats != 5_000_000_000 || summary.TotalSats != 5_000_000_000 || len(summary.Outputs) != 0 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestAnalyzeCoinbaseSourceDerivesOnlyDashboardEvidence(t *testing.T) {
	workerScript, _ := hex.DecodeString("76a914111111111111111111111111111111111111111188ac")
	workerHash := sha256.Sum256(workerScript)
	source := model.CoinbaseSource{
		Coinbase1:   "0100000001" + zeroHex(32) + "ffffffff0c03a1bb0d",
		Coinbase2:   "ffffffff01" + "205fa01200000000" + "19" + hex.EncodeToString(workerScript) + "00000000",
		ExtraNonce1: "01020304", ExtraNonce2Size: 4,
		WorkerScriptSHA256: hex.EncodeToString(workerHash[:]),
	}
	height, evidence, err := AnalyzeCoinbaseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if height != 900_000 || !evidence.WorkerWalletInCoinbase || evidence.CoinbaseTotalSats != 312_500_000 || evidence.WorkerPayoutSats != 312_500_000 || evidence.EstimatedPoolFeePct == nil || *evidence.EstimatedPoolFeePct != 0 {
		t.Fatalf("height=%d evidence=%+v", height, evidence)
	}
}

func TestAnalyzeWitnessCoinbase(t *testing.T) {
	workerScript, _ := hex.DecodeString("76a914222222222222222222222222222222222222222288ac")
	raw, _ := hex.DecodeString("02000000000101" + zeroHex(32) + "ffffffff0100ffffffff01" + "0100000000000000" + "19" + hex.EncodeToString(workerScript) + "0120" + zeroHex(32) + "00000000")
	summary, err := analyzeCoinbase(raw, workerScript)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.WorkerWalletSeen || summary.TotalSats != 1 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestAnalyzeCoinbaseRejectsTrailingData(t *testing.T) {
	raw, _ := hex.DecodeString("0100000001" + zeroHex(32) + "ffffffff0100ffffffff0100000000000000000000000000ff")
	if _, err := analyzeCoinbase(raw, nil); err == nil {
		t.Fatal("accepted trailing data")
	}
}

func TestDescribeOutputScriptUsesBech32AndBech32m(t *testing.T) {
	tests := []struct {
		script, address, scriptType string
	}{
		{
			script:     "0014751e76e8199196d454941c45d1b3a323f1433bd6",
			address:    "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
			scriptType: "p2wpkh",
		},
		{
			script:     "512079be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
			address:    "bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0",
			scriptType: "p2tr",
		},
	}
	for _, test := range tests {
		script, err := hex.DecodeString(test.script)
		if err != nil {
			t.Fatal(err)
		}
		address, scriptType := describeOutputScript(script)
		if address != test.address || scriptType != test.scriptType {
			t.Errorf("describeOutputScript(%s) = %q, %q; want %q, %q", test.script, address, scriptType, test.address, test.scriptType)
		}
	}
}

func TestAnalyzeCoinbaseBoundsRetainedDestinations(t *testing.T) {
	var raw bytes.Buffer
	if err := binary.Write(&raw, binary.LittleEndian, uint32(1)); err != nil {
		t.Fatal(err)
	}
	raw.WriteByte(1)
	raw.Write(make([]byte, 32))
	if err := binary.Write(&raw, binary.LittleEndian, uint32(0xffffffff)); err != nil {
		t.Fatal(err)
	}
	raw.WriteByte(1)
	raw.WriteByte(0)
	if err := binary.Write(&raw, binary.LittleEndian, uint32(0xffffffff)); err != nil {
		t.Fatal(err)
	}
	raw.WriteByte(maxCoinbaseOutputsStored + 1)
	for i := 0; i <= maxCoinbaseOutputsStored; i++ {
		if err := binary.Write(&raw, binary.LittleEndian, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
		raw.WriteByte(1)
		raw.WriteByte(byte(i + 1))
	}
	if err := binary.Write(&raw, binary.LittleEndian, uint32(0)); err != nil {
		t.Fatal(err)
	}

	summary, err := analyzeCoinbase(raw.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.OutputCount != maxCoinbaseOutputsStored+1 || len(summary.Outputs) != maxCoinbaseOutputsStored || !summary.OutputsTruncated || summary.OmittedSats != 1 {
		t.Fatalf("bounded summary=%+v", summary)
	}
	if summary.Outputs[0].ValueSats != maxCoinbaseOutputsStored+1 {
		t.Fatalf("largest retained output=%d", summary.Outputs[0].ValueSats)
	}
}

func TestAnalyzeCoinbaseDoesNotTreatZeroValueWorkerScriptAsPayout(t *testing.T) {
	workerScript, err := hex.DecodeString("76a914333333333333333333333333333333333333333388ac")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString("0100000001" + zeroHex(32) + "ffffffff0100ffffffff01" + "0000000000000000" + "19" + hex.EncodeToString(workerScript) + "00000000")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := analyzeCoinbase(raw, workerScript)
	if err != nil {
		t.Fatal(err)
	}
	if summary.WorkerWalletSeen || summary.WorkerSats != 0 || len(summary.Outputs) != 0 {
		t.Fatalf("zero-value worker script counted as payout: %+v", summary)
	}
}
