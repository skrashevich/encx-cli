package encx

import (
	"strings"
	"testing"
)

func TestDecodeGameModelJSONEmptyBody(t *testing.T) {
	_, err := decodeGameModelJSON(nil, "game model")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}
