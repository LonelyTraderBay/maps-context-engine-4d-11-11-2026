package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestSerializedBudgetFixedPointAndNoConvergence(t *testing.T) {
	b := Budget{MaxOutputBytes: 4096, DeliveryMode: "mcp_dual"}
	calls := 0
	settled, blocks, err := SettleSerializedBudget(b, func(n int64) ([][]byte, error) {
		calls++
		data := []byte(fmt.Sprintf(`{"serialized_bytes":%d}`, n))
		return [][]byte{data, data}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls > 8 || settled.SerializedBytes != int64(len(blocks[0])*2) || !strings.Contains(string(blocks[0]), fmt.Sprintf(`:%d}`, settled.SerializedBytes)) {
		t.Fatal("incorrect fixed point")
	}
	if err = ValidateDeliveredBytes(settled, blocks); err != nil {
		t.Fatal(err)
	}
	calls = 0
	_, _, err = SettleSerializedBudget(Budget{MaxOutputBytes: 4096, DeliveryMode: "http"}, func(n int64) ([][]byte, error) {
		calls++
		if n == 1 {
			return [][]byte{[]byte("xx")}, nil
		}
		return [][]byte{[]byte("x")}, nil
	})
	if err == nil || calls != 8 {
		t.Fatalf("expected bounded nonconvergence: %d %v", calls, err)
	}
}
