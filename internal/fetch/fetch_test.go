package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnAddressThatIsNotHTTPSIsRefusedBeforeAnyRequest(t *testing.T) {
	if _, err := Get(&http.Client{}, "http://example.test/file", ""); err == nil {
		t.Fatal("a plain http address was read")
	}
}

func TestModifiedRefusesAnAddressThatIsNotHTTPS(t *testing.T) {
	if _, err := Modified(&http.Client{}, "http://example.test/file"); err == nil {
		t.Fatal("a plain http address was asked")
	}
}

func TestPinnedReadsOnlyTheRecordedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "nomenclature")
	}))
	defer server.Close()
	sum := sha256.Sum256([]byte("nomenclature"))
	data, err := Pinned(server.Client(), server.URL+"/cn.xml", 100, hex.EncodeToString(sum[:]))
	if err != nil || string(data) != "nomenclature" {
		t.Fatalf("got %q, %v", data, err)
	}
	other := sha256.Sum256([]byte("other"))
	if _, err := Pinned(server.Client(), server.URL+"/cn.xml", 100, hex.EncodeToString(other[:])); err == nil {
		t.Error("a file with another sum was read")
	}
	if _, err := Pinned(server.Client(), server.URL+"/cn.xml", 5, hex.EncodeToString(sum[:])); err == nil {
		t.Error("a file over the limit was read")
	}
	if _, err := Pinned(server.Client(), server.URL+"/cn.xml", 100, "abc"); err == nil {
		t.Error("a sum that is not one was taken")
	}
}
