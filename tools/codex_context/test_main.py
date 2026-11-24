import json
from subprocess import CompletedProcess
import unittest
from unittest.mock import patch

import main

from main import GO_EVIDENCE_BYTES, GO_EVIDENCE_EXCERPTS, go_evidence_json, prepare_prompt


class PreparePromptTest(unittest.TestCase):
    def test_go_source_preserves_physical_numbers_across_blank_lines(self):
        member = {"worktree_id": "worktree", "profile_id": "profile", "generation_id": "generation"}
        response = {
            "scope": {"members": [member]},
            "coverage": {"gaps": [{"code": "GO_QUESTION_CANDIDATES"}]},
            "data": {"excerpts": [{
                "source_ref": {**member, "path": "example.go"},
                "start_line": 70,
                "content": "case 36:\r\n\r\n\treturn result\r\n",
            }]},
        }
        result = CompletedProcess([], 6, stdout=json.dumps(response).encode())
        with patch("main.subprocess.run", return_value=result):
            prompt, _ = prepare_prompt("Đoạn nào trả kết quả?", "mce4d", "worktree", "profile")
        self.assertEqual(json.loads(prompt.split("\n")[-1]), [{
            "path": "example.go",
            "lines": [[70, "case 36:"], [71, ""], [72, "\treturn result"]],
        }])

    def test_go_prompt_budget_preserves_rank_and_whole_lines(self):
        excerpts = [
            {"source_ref": {"path": f"rank{i}.go"}, "start_line": 40,
             "content": "\t// " + "ữ" * 400 + "\n"}
            for i in range(20)
        ]
        evidence = go_evidence_json(excerpts)
        self.assertLessEqual(len(evidence.encode("utf-8")), GO_EVIDENCE_BYTES)
        sections = json.loads(evidence)
        self.assertGreater(len(sections), 0)
        self.assertLess(len(sections), GO_EVIDENCE_EXCERPTS)
        self.assertEqual(sections, [
            {"path": e["source_ref"]["path"], "lines": [[40, e["content"].removesuffix("\n")]]}
            for e in excerpts[:len(sections)]
        ])

class GoEditEvidenceTest(unittest.TestCase):
    def test_message_uses_exact_literal_and_count_in_one_packet(self):
        source = (
            'func checkRange() error {\n'
            '\tif !boundary() {\n'
            '\t\treturn fmt.Errorf("range splits UTF-8 encoding")\n'
            '\t}\n'
            '\treturn fmt.Errorf("captured content hash mismatch")\n'
            '}\n'
        )
        excerpt = {"source_ref": {"path": "source.go"}, "start_line": 1, "content": source}
        result = main.prepare_go_edit_evidence(
            "Khi byte range cắt đứt một ký tự UTF-8, đổi thông báo đó thành 'source range splits UTF-8'.",
            [excerpt], {"source.go": source.encode()},
        )
        self.assertEqual(result["kind"], "message")
        self.assertEqual(result["literal"], '"range splits UTF-8 encoding"')
        self.assertEqual(result["occurrences"], 1)
        self.assertEqual(result["lines"][2], [3, '\t\treturn fmt.Errorf("range splits UTF-8 encoding")'])

    def test_field_groups_all_repeated_sites_by_exact_source(self):
        model = 'type ValidateData struct {\n\tRecordSetChanged bool `json:"record_set_changed"`\n}\n'
        model += 'func verify(r ValidateData) bool { return !r.RecordSetChanged }\n'
        generated = "".join(
            f"func use{i}(v ValidateData) bool {{ return v.RecordSetChanged }}\n" for i in range(129)
        )
        excerpt = {"source_ref": {"path": "model.go"}, "start_line": 1, "content": model}
        args = ("Đổi tên trường báo tập record đã thay đổi thành RecordsChanged.", [excerpt])
        result = main.prepare_go_edit_evidence(*args, {"model.go": model.encode(), "generated.go": generated.encode()})
        self.assertEqual(result["kind"], "field_text_matches")
        self.assertEqual(result["identifier"], "RecordSetChanged")
        files = {group["path"]: group for group in result["files"]}
        self.assertEqual(files["model.go"]["occurrences"], 2)
        self.assertIn('json:"record_set_changed"', files["model.go"]["groups"][0]["text"])
        group, = files["generated.go"]["groups"]
        self.assertEqual((group["line"], group["text"], group["occurrences"]),
                         (1, "(v ValidateData) bool { return v.RecordSetChanged }", 129))
        self.assertEqual(group["context_start_line"], 1)
        self.assertEqual(group["context"], "\n".join(generated.splitlines()[:2]))

    def test_ambiguous_field_uses_normal_excerpts(self):
        model = "type A struct {\n\tOldRecordsChanged bool\n\tSomeRecordsChanged bool\n}\n"
        excerpt = {"source_ref": {"path": "model.go"}, "start_line": 1, "content": model}
        self.assertIsNone(main.prepare_go_edit_evidence(
            "Đổi tên trường thành RecordsChanged.", [excerpt], {"model.go": model.encode()},
        ))

    def test_stale_message_excerpt_cannot_produce_a_count(self):
        captured = 'return fmt.Errorf("range splits UTF-8 encoding")\n'
        excerpt = {"source_ref": {"path": "check.go"}, "start_line": 1, "content": captured}
        self.assertIsNone(main.prepare_go_edit_evidence(
            "Đổi thông báo thành 'source range splits UTF-8'.",
            [excerpt], {"check.go": b"// The captured text only appears below.\n" + captured.encode()},
        ))

    def test_interface_groups_include_scope_context_for_same_text(self):
        local = "package index\ntype Reader interface { Read() }\nfunc locate(r Reader) {}\n"
        excerpt = {"source_ref": {"path": "index/api.go"}, "start_line": 2,
                   "content": "type Reader interface { Read() }\n"}
        result = main.prepare_go_edit_evidence(
            "Đổi tên chính interface chỉ đọc thành IndexReader. Cập nhật mọi nơi tham chiếu.",
            [excerpt],
            {"index/api.go": local.encode(), "index/client/pick.go": b"package client\nfunc pick(r index.Reader) {}\n",
             "codec/io.go": b"package codec\nfunc copyFrom(r io.Reader) {}\n"},
        )
        self.assertEqual(result["kind"], "interface_text_matches")
        self.assertEqual(result["identifier"], "Reader")
        self.assertEqual(result["declaration"], "index/api.go")
        self.assertEqual([(f["path"], f["occurrences"]) for f in result["files"]],
                         [("codec/io.go", 1), ("index/api.go", 2), ("index/client/pick.go", 1)])
