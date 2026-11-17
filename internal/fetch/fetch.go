// Package fetch reads one public file over HTTPS for a build.
package fetch

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
	request.Header.Set("User-Agent", "romanian-datasets (https://github.com/pidginhost/romanian-datasets)")
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
