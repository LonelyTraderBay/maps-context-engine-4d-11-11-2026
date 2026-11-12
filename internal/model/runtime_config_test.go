package model

import "testing"

func TestRuntimeConfigDefaultsAndConstraints(t *testing.T) {
	config, err := DecodeRuntimeConfigJSON([]byte(`{"schema_version":1}`), 8192, 8)
	if err != nil {
		t.Fatal(err)
	}
	if config.Core.MaxFileBytes != 2097152 || config.Core.MaxParseWorkers != 4 || config.Explorer.GraphDefaultNodes != 200 {
		t.Fatalf("wrong defaults: %+v", config)
	}
	config, err = DecodeRuntimeConfigJSON([]byte(`{"schema_version":1,"core":{"max_file_bytes":100,"max_parse_workers":0},"explorer":{"graph_default_nodes":10}}`), 8192, 2)
	if err != nil || config.Core.MaxFileBytes != 100 || config.Core.MaxParseWorkers != 2 || config.Explorer.GraphDefaultNodes != 10 {
		t.Fatalf("merge: %+v %v", config, err)
	}
	for _, bad := range []string{
		`{}`, `{"schema_version":2}`, `{"schema_version":1,"core":{"unknown":1}}`, `{"schema_version":1,"core":{"MAX_FILE_BYTES":10}}`, `{"schema_version":1,"core":{"max_file_bytes":null}}`,
		`{"schema_version":1,"core":{"max_file_bytes":10,"max_source_bytes_per_snapshot":9}}`,
		`{"schema_version":1,"core":{"watch_poll_interval_ms":20,"full_reconcile_interval_ms":10}}`,
		`{"schema_version":1,"core":{"rename_result_max_bytes":20,"rename_results_max_bytes":10}}`,
		`{"schema_version":1,"explorer":{"graph_default_nodes":20,"graph_hard_nodes":10}}`,
		`{"schema_version":1,"core":{"max_output_bytes":131073}}`,
	} {
		if _, err := DecodeRuntimeConfigJSON([]byte(bad), 8192, 4); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if _, err := DecodeRuntimeConfigJSON([]byte(`{"schema_version":1,"core":{"max_parse_workers":3}}`), 8192, 0); err != nil {
		t.Fatal("explicit workers should not require CPU discovery", err)
	}
	if _, err := DecodeRuntimeConfigJSON([]byte(`{"schema_version":1}`), 8192, 0); err == nil {
		t.Fatal("accepted unknown CPU count")
	}
}
