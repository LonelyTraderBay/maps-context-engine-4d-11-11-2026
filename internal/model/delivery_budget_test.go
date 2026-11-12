package model

import "testing"

func TestDeliveredBudgetCountsBothMCPRepresentations(t *testing.T) {
	text := []byte("a😀")
	structured := []byte(`{"text":"a😀"}`)
	b := Budget{MaxOutputBytes: 4096, SerializedBytes: int64(len(text) + len(structured)), DeliveryMode: "mcp_dual"}
	if err := ValidateDeliveredBytes(b, [][]byte{text, structured}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDeliveredBytes(b, [][]byte{text}); err == nil {
		t.Fatal("accepted missing dual representation")
	}
	b.SerializedBytes = int64(len(text))
	if err := ValidateDeliveredBytes(b, [][]byte{text, structured}); err == nil {
		t.Fatal("accepted missing duplicate byte count")
	}
	b.SerializedBytes = 4096
	if err := ValidateDeliveredBytes(b, [][]byte{make([]byte, 4096), []byte("x")}); err == nil {
		t.Fatal("accepted over cap")
	}
}
