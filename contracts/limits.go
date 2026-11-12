package contracts

// RequestHardLimit is the contract byte cap for the registered operation. The
// transport must still enforce it before allocating or reading the full body.
func (op Operation) RequestHardLimit() int {
	switch op.Name {
	case "POST /session/bootstrap", "POST /preview/attach", "POST /preview/bind", "POST /preview/mode", "POST /preview/ack", "POST /preview/invalidate", "POST /preview/detach", "POST /preview/status", "POST /mapping/propose", "POST /mapping/review", "POST /records/list", "POST /records/read", "CLI records list", "CLI records read":
		return 16384
	case "POST /graph/query", "POST /flow/query":
		return 65536
	default:
		return 32768
	}
}
func (op Operation) responseHardLimit() int {
	if op.Name == "POST /graph/query" || op.Name == "POST /flow/query" {
		return 1048576
	}
	return 131072
}
