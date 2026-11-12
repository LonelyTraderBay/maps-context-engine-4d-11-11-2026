CREATE TABLE evidence_artifacts (
 artifact_id TEXT PRIMARY KEY NOT NULL CHECK(length(artifact_id)=64),
 artifact_kind TEXT NOT NULL CHECK(artifact_kind IN('runtime_trace','test_report','coverage_report','deployment_metadata')),
 input_hash TEXT NOT NULL CHECK(length(input_hash)=64),
 sanitizer_version TEXT NOT NULL,
 metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
 created_at TEXT NOT NULL
) STRICT;
CREATE TABLE evidence_items (
 artifact_id TEXT NOT NULL REFERENCES evidence_artifacts(artifact_id) ON DELETE CASCADE,
 item_id TEXT NOT NULL, kind TEXT NOT NULL,
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 source_binding_json TEXT NOT NULL CHECK(json_valid(source_binding_json)),
 PRIMARY KEY(artifact_id,item_id)
) STRICT;
CREATE INDEX evidence_kind ON evidence_items(artifact_id,kind,item_id);
UPDATE schema_meta SET schema_version=2,min_reader_version=2 WHERE singleton=1;
