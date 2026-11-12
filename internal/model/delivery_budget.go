package model

import "fmt"

func deliveredBytes(b Budget, blocks [][]byte) (int64, error) {
	want := 1
	if b.DeliveryMode == "mcp_dual" {
		want = 2
	}
	switch b.DeliveryMode {
	case "http", "cli", "mcp_text", "mcp_dual", "mcp_structured_verified":
	default:
		return 0, fmt.Errorf("invalid delivery mode")
	}
	if b.MaxOutputBytes < 4096 || b.MaxOutputBytes > 1048576 || len(blocks) != want {
		return 0, fmt.Errorf("invalid delivery budget or blocks")
	}
	var total int64
	for _, block := range blocks {
		if len(block) == 0 || int64(len(block)) > b.MaxOutputBytes-total {
			return 0, fmt.Errorf("delivery exceeds budget or is empty")
		}
		total += int64(len(block))
	}
	return total, nil
}

// ValidateDeliveredBytes checks caller-supplied tool-visible serialized blocks;
// framing, hidden transport bytes and token estimates are outside its scope.
// Actual adapter integration must supply exactly the data delivered to the host.
func ValidateDeliveredBytes(b Budget, blocks [][]byte) error {
	total, err := deliveredBytes(b, blocks)
	if err != nil {
		return err
	}
	if total != b.SerializedBytes {
		return fmt.Errorf("serialized byte count mismatch")
	}
	return nil
}

// SettleSerializedBudget invokes the pure serializer with the current count,
// starting at zero, at most eight times. The serializer must put the supplied
// count into the envelope and return exactly the representations for delivery.
// It neither emits bytes nor proves a host consumed them.
func SettleSerializedBudget(b Budget, serialize func(int64) ([][]byte, error)) (Budget, [][]byte, error) {
	if serialize == nil {
		return Budget{}, nil, fmt.Errorf("missing serializer")
	}
	estimate := int64(0)
	for i := 0; i < 8; i++ {
		blocks, err := serialize(estimate)
		if err != nil {
			return Budget{}, nil, err
		}
		size, err := deliveredBytes(b, blocks)
		if err != nil {
			return Budget{}, nil, err
		}
		if size == estimate {
			b.SerializedBytes = size
			return b, blocks, nil
		}
		estimate = size
	}
	return Budget{}, nil, fmt.Errorf("serialized budget did not converge")
}
