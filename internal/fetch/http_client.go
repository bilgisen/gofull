// FILE: internal/fetch/http_client.go
package fetch

import (
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// ClientOptions for the fetch client.
type ClientOptions struct {
	Timeout   time.Duration
	UserAgent string
}

// Client is a small wrapper around retryablehttp to provide timeouts and UA.
type Client struct {
	inner *retryablehttp.Client
}

// NewClient creates a new Client.
func NewClient(opts ClientOptions) *Client {
	r := retryablehttp.NewClient()
	r.RetryMax = 2
	r.HTTPClient.Timeout = opts.Timeout
	// default backoff and logger are fine
	return &Client{inner: r}
}

// Get returns the HTTP response body bytes via http.Get-style convenience.
func (c *Client) Get(url string, headers map[string]string) (*http.Response, error) {
	req, err := retryablehttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.inner.StandardClient().Do(req.Request)
}