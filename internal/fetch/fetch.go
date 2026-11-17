// Package fetch reads one public file over HTTPS for a build.
package fetch

import (
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
