package search

import (
	"context"
	"strings"
	"testing"
)

func TestGoQuestionFindsStatementDeepInBody(t *testing.T) {
	const wanted = "if now >= expiresAt {"
	source := "package sample\nfunc assessValidity() {\n" +
		strings.Repeat("\t_ = true\n", 44) +
		"\t" + wanted + "\n\t\treturn\n\t}\n}\n"
	files := []GoQuestionFile{
		{Path: "clock.go", Bytes: []byte(source)},
		{Path: "usage.go", Bytes: []byte("package sample\n// Deadline is tracked elsewhere.\nfunc names() {}\n")},
	}
	q := NewGoQuestion("Khi quá hạn, điều kiện nào kiểm tra thời điểm hết hạn?")
	matches, err := q.Sections(context.Background(), files, 3)
	if err != nil || len(matches) == 0 {
		t.Fatalf("no candidates: %v", err)
	}
	for _, hit := range matches {
		if hit.Path == "clock.go" && strings.Contains(source[hit.Range.StartByte:hit.Range.EndByte], wanted) {
			return
		}
	}
	t.Fatalf("condition missing from selected sections: %+v", matches)
}

func TestGoEditQuestionFindsDefinitionsAndSimilarValidators(t *testing.T) {
	pathCheck := "(path string) bool {\n for _, segment := range strings.Split(path, \"/\") {\n if segment == \".\" { return false }\n }; return true\n}\n"
	files := []GoQuestionFile{
		{Path: "preview_location.go", Bytes: []byte("package p\nfunc validPreviewPath" + pathCheck)},
		{Path: "source_ref.go", Bytes: []byte("package p\ntype SourceRef struct{}\nfunc (ref SourceRef) Validate() bool { return validSourcePath(ref.Path) }\nfunc validSourcePath" + pathCheck)},
		{Path: "cursor.go", Bytes: []byte("package p\nconst cursorTTL = 15 * time.Minute\nfunc checkExpiry(expires time.Time) bool { return time.Until(expires) < cursorTTL }\n")},
		{Path: "records.go", Bytes: []byte("package p\ntype ValidateData struct { RecordSetChanged bool }\nfunc verify(data ValidateData) bool { return !data.RecordSetChanged }\n")},
	}
	for _, tc := range []struct {
		question string
		wanted   string
	}{
		{"Trong kiểm tra đường dẫn của tham chiếu mã nguồn, tìm vòng lặp kiểm tra từng thành phần tách bằng dấu gạch chéo.", "func validSourcePath"},
		{"Đổi TTL dùng chung của cursor phân trang đã ký thành đúng 90 giây. Giữ cấu trúc tính thời điểm hết hạn.", "const cursorTTL"},
		{"Trong kiểu kết quả xác thực gói ngữ cảnh, đổi tên trường Go báo tập record đã thay đổi thành RecordsChanged.", "type ValidateData"},
	} {
		hits, err := NewGoQuestion(tc.question).Sections(context.Background(), files, 3)
		if err != nil || len(hits) == 0 {
			t.Fatalf("%s: no results: %v", tc.question, err)
		}
		first := hits[0]
		found := false
		for _, file := range files {
			if first.Path == file.Path {
				found = strings.Contains(string(file.Bytes[first.Range.StartByte:first.Range.EndByte]), tc.wanted)
			}
		}
		if !found {
			t.Errorf("%s: missing %q from the first excerpt: %+v", tc.question, tc.wanted, hits)
		}
	}
}

func TestGoQuestionPrioritizesVersionedTimeAndStringParsers(t *testing.T) {
	tests := []struct {
		question string
		files    []GoQuestionFile
		wanted   string
	}{
		{
			"Trong bộ sinh thời gian UUID v7, khi đồng hồ logic không tăng, dùng giá trị trước cộng 2. Giữ tách mili giây và số thứ tự.",
			[]GoQuestionFile{
				{Path: "time.go", Bytes: []byte("package p\n// ClockSequence returns the sequence for the clock.\nfunc ClockSequence() int { return clockSequence }\n")},
				{Path: "version7.go", Bytes: []byte("package p\nfunc getV7Time() (milli, seq int64) { if now <= lastV7time { now = lastV7time + 1 }; return milli, seq }\n")},
			},
			"func getV7Time",
		},
		{
			"Khi sinh timestamp cho UUID v1 và đồng hồ không tiến, tăng clock sequence thêm 2 thay vì 1. Giữ phép mask.",
			[]GoQuestionFile{
				{Path: "time.go", Bytes: []byte("package p\n// ClockSequence returns the clock sequence used for Version 1 UUIDs.\nfunc ClockSequence() int { return clockSequence }\nfunc getTime() (Time, uint16, error) {\n // If the clock went backwards, increment the sequence.\n if now <= lasttime { clockSeq = ((clockSeq + 1) & 0x3fff) | 0x8000 }; return Time(now), clockSeq, nil\n}\n")},
			},
			"func getTime",
		},
		{
			"Trong đường đọc chuỗi kích thước byte ra uint64, đổi lỗi đơn vị lạ. Giữ nguyên nhánh tràn và đọc số nguyên lớn.",
			[]GoQuestionFile{
				{Path: "bytes.go", Bytes: []byte("package p\n// BytesN uses the given number of digits to report a size.\nfunc BytesN(s uint64, n int) string { return format(s, n) }\nfunc ParseBytes(s string) (uint64, error) { return 0, fmt.Errorf(\"unhandled size name: %v\", s) }\n")},
			},
			"func ParseBytes",
		},
	}
	for _, tc := range tests {
		got, err := NewGoQuestion(tc.question).Sections(context.Background(), tc.files, 1)
		if err != nil || len(got) != 1 {
			t.Fatalf("no match for %q: %v", tc.question, err)
		}
		match := got[0]
		for _, file := range tc.files {
			if file.Path == match.Path && !strings.Contains(string(file.Bytes[match.Range.StartByte:match.Range.EndByte]), tc.wanted) {
				t.Errorf("%q: first section = %q, want %q", tc.question, file.Bytes[match.Range.StartByte:match.Range.EndByte], tc.wanted)
			}
		}
	}
}

func TestGoQuestionConnectsGroupedNumbersToGoLimits(t *testing.T) {
	files := []GoQuestionFile{
		{Path: "format.go", Bytes: []byte("package p\nfunc EditBudget(edits []Edit) bool { return len(edits) > 200 }\n")},
		{Path: "limit.go", Bytes: []byte("package p\nfunc check(editCount int) bool { return editCount > 10000 }\n")},
	}
	hits, err := NewGoQuestion("Hạ số edit tối đa từ 10.000 xuống 4.096.").Sections(context.Background(), files, 1)
	if err != nil || len(hits) != 1 || hits[0].Path != "limit.go" {
		t.Fatalf("grouped old bound should match 10000 first: %+v, %v", hits, err)
	}
}
