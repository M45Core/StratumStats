package probe

import (
	"encoding/hex"
	"testing"
)

func TestAnalyzeCoinbaseFindsWorkerOutputs(t *testing.T) {
	workerScript, _ := hex.DecodeString("76a914111111111111111111111111111111111111111188ac")
	raw, _ := hex.DecodeString("0100000001" + zeroHex(32) + "ffffffff0100ffffffff01" + "00f2052a01000000" + "19" + hex.EncodeToString(workerScript) + "00000000")
	summary, err := analyzeCoinbase(raw, workerScript)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.WorkerWalletSeen || summary.WorkerSats != 5_000_000_000 || summary.TotalSats != 5_000_000_000 {
		t.Fatalf("summary=%+v", summary)
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
