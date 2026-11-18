// Package fetch reads one public file over HTTPS for a build, or over plain HTTP when a person recorded the
// SHA-256 of the file.
package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// userAgent names the build to the sites it reads.
const userAgent = "romanian-datasets (https://github.com/pidginhost/romanian-datasets)"

// Get gives the body of a file. The address must be https and the answer must be 200.
// The caller closes the body and limits how much of it is read.
func Get(client *http.Client, address, accept string) (io.ReadCloser, error) {
	if !strings.HasPrefix(address, "https://") {
		return nil, fmt.Errorf("%s is not an https address", address)
	}
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	return response.Body, nil
}

// Modified gives the time a file was last changed, from the Last-Modified header of a HEAD request. The
// address must be https and the answer must be 200 with that header.
func Modified(client *http.Client, address string) (time.Time, error) {
	if !strings.HasPrefix(address, "https://") {
		return time.Time{}, fmt.Errorf("%s is not an https address", address)
	}
	request, err := http.NewRequest(http.MethodHead, address, nil)
	if err != nil {
		return time.Time{}, err
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		return time.Time{}, err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("%s answered %s", address, response.Status)
	}
	modified, err := http.ParseTime(response.Header.Get("Last-Modified"))
	if err != nil {
		return time.Time{}, fmt.Errorf("%s has no Last-Modified date: %w", address, err)
	}
	return modified, nil
}

// read gets a file over http or https and reads at most limit bytes of it.
func read(client *http.Client, address string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
		return nil, fmt.Errorf("%s is not a web address", address)
	}
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", address)
	}
	return data, nil
}

// Pinned gives the body of a file whose SHA-256 a person recorded, refusing any other. It is the one way a
// build reads data over plain http, for a source that offers no https: the recorded sum, not the
// connection, is what makes the file trusted. The answer must be 200 and no larger than limit.
func Pinned(client *http.Client, address string, limit int64, sum string) ([]byte, error) {
	if len(sum) != 64 {
		return nil, fmt.Errorf("%s: the recorded SHA-256 %q is not one", address, sum)
	}
	data, err := read(client, address, limit)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != sum {
		return nil, fmt.Errorf("%s has SHA-256 %x, not the recorded %s: a person checks the new file and records its sum", address, got, sum)
	}
	return data, nil
}

// Unchecked gives the body of a page over http or https without a recorded sum. Nothing it gives may be
// published: a build only looks in it for a sign that a person has work to do, as a new file listed.
func Unchecked(client *http.Client, address string, limit int64) ([]byte, error) {
	return read(client, address, limit)
}
