package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestPackEvidenceFragmentIdentity(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := []byte("a😀\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(source))
	parent := SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: hash, Range: ByteRange{StartByte: 0, EndByte: int64(len(source))}}
	child := parent
	child.Range = ByteRange{StartByte: 1, EndByte: 5}
	itemID, err := SourceFragmentID(parent, child.Range, source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	parentRaw := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"a.go","content_hash":"%s","range":{"start_byte":0,"end_byte":%d}}`, id, h, id, hash, len(source))
	childRaw := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"a.go","content_hash":"%s","range":{"start_byte":1,"end_byte":5}}`, id, h, id, hash)
	item := fmt.Sprintf(`{"item_id":"%s","kind":"source_range_fragment","required_for_review":true,"why_included":"target","source_refs":[%s],"content":"😀","evidence_refs":[],"limitations":[],"fragment_of":%s}`, itemID, childRaw, parentRaw)
	raw := fmt.Sprintf(`{"operation":"context_pack","pack_token":"token","target_refs":[],"items":[%s],"edit_precondition_validated":false}`, item)
	pack, err := DecodePackDataJSON([]byte(raw), 32768)
	if err != nil {
		t.Fatal(err)
	}
	if err := pack.Items[0].ValidateFragment(source, 8192); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(raw, `"fragment_of":`+parentRaw, `"fragment_of":null`, 1)
	if _, err := DecodePackDataJSON([]byte(bad), 32768); err == nil {
		t.Fatal("accepted fragment without parent")
	}
}
