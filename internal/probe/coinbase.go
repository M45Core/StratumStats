package probe

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

type coinbaseSummary struct {
	TotalSats        uint64
	WorkerSats       uint64
	WorkerWalletSeen bool
}

// analyzeCoinbase parses both legacy and witness transaction serialization.
// Matching uses the exact scriptPubKey generated for this probe session.
func analyzeCoinbase(raw, workerScript []byte) (coinbaseSummary, error) {
	var result coinbaseSummary
	cursor := 0
	take := func(n int) ([]byte, error) {
		if n < 0 || cursor > len(raw)-n {
			return nil, fmt.Errorf("coinbase truncated at byte %d", cursor)
		}
		value := raw[cursor : cursor+n]
		cursor += n
		return value, nil
	}
	if _, err := take(4); err != nil {
		return result, err
	}
	witness := false
	if len(raw)-cursor >= 2 && raw[cursor] == 0x00 && raw[cursor+1] != 0x00 {
		witness = true
		cursor += 2
	}
	inputs, err := readCompactSize(raw, &cursor)
	if err != nil {
		return result, fmt.Errorf("input count: %w", err)
	}
	if inputs == 0 || inputs > 1000 {
		return result, fmt.Errorf("implausible input count %d", inputs)
	}
	for i := uint64(0); i < inputs; i++ {
		if _, err := take(36); err != nil {
			return result, err
		}
		scriptLen, err := readCompactSize(raw, &cursor)
		if err != nil {
			return result, err
		}
		if scriptLen > uint64(len(raw)) {
			return result, fmt.Errorf("input script too large")
		}
		if _, err := take(int(scriptLen) + 4); err != nil {
			return result, err
		}
	}
	outputs, err := readCompactSize(raw, &cursor)
	if err != nil {
		return result, fmt.Errorf("output count: %w", err)
	}
	if outputs == 0 || outputs > 10000 {
		return result, fmt.Errorf("implausible output count %d", outputs)
	}
	for i := uint64(0); i < outputs; i++ {
		valueBytes, err := take(8)
		if err != nil {
			return result, err
		}
		value := binary.LittleEndian.Uint64(valueBytes)
		if ^uint64(0)-result.TotalSats < value {
			return result, fmt.Errorf("output value overflow")
		}
		result.TotalSats += value
		scriptLen, err := readCompactSize(raw, &cursor)
		if err != nil {
			return result, err
		}
		if scriptLen > uint64(len(raw)) {
			return result, fmt.Errorf("output script too large")
		}
		script, err := take(int(scriptLen))
		if err != nil {
			return result, err
		}
		if len(workerScript) > 0 && bytes.Equal(script, workerScript) {
			result.WorkerWalletSeen = true
			if ^uint64(0)-result.WorkerSats < value {
				return result, fmt.Errorf("worker output overflow")
			}
			result.WorkerSats += value
		}
	}
	if witness {
		for i := uint64(0); i < inputs; i++ {
			items, err := readCompactSize(raw, &cursor)
			if err != nil {
				return result, err
			}
			if items > 1000 {
				return result, fmt.Errorf("implausible witness item count")
			}
			for j := uint64(0); j < items; j++ {
				itemLen, err := readCompactSize(raw, &cursor)
				if err != nil {
					return result, err
				}
				if itemLen > uint64(len(raw)) {
					return result, fmt.Errorf("witness item too large")
				}
				if _, err := take(int(itemLen)); err != nil {
					return result, err
				}
			}
		}
	}
	if _, err := take(4); err != nil {
		return result, fmt.Errorf("locktime: %w", err)
	}
	if cursor != len(raw) {
		return result, fmt.Errorf("coinbase has %d trailing bytes", len(raw)-cursor)
	}
	return result, nil
}

func readCompactSize(raw []byte, cursor *int) (uint64, error) {
	if *cursor >= len(raw) {
		return 0, fmt.Errorf("missing compact size")
	}
	prefix := raw[*cursor]
	*cursor++
	switch prefix {
	case 0xfd:
		if *cursor > len(raw)-2 {
			return 0, fmt.Errorf("truncated uint16")
		}
		value := uint64(binary.LittleEndian.Uint16(raw[*cursor : *cursor+2]))
		*cursor += 2
		if value < 0xfd {
			return 0, fmt.Errorf("non-canonical compact size")
		}
		return value, nil
	case 0xfe:
		if *cursor > len(raw)-4 {
			return 0, fmt.Errorf("truncated uint32")
		}
		value := uint64(binary.LittleEndian.Uint32(raw[*cursor : *cursor+4]))
		*cursor += 4
		if value <= 0xffff {
			return 0, fmt.Errorf("non-canonical compact size")
		}
		return value, nil
	case 0xff:
		if *cursor > len(raw)-8 {
			return 0, fmt.Errorf("truncated uint64")
		}
		value := binary.LittleEndian.Uint64(raw[*cursor : *cursor+8])
		*cursor += 8
		if value <= 0xffffffff {
			return 0, fmt.Errorf("non-canonical compact size")
		}
		return value, nil
	default:
		return uint64(prefix), nil
	}
}
