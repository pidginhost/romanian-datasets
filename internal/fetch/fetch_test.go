package fetch

import (
	"net/http"
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
