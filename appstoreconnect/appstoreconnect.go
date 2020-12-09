package appstoreconnect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/bitrise-io/bitrise-add-new-project/httputil"
	"github.com/bitrise-io/go-utils/log"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/google/go-querystring/query"
)

const (
	baseURL    = "https://api.appstoreconnect.apple.com/"
	apiVersion = "v1"
)

// HTTPClient ...
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type service struct {
	client *Client
}

// Client communicate with the Apple API
type Client struct {
	EnableDebugLogs bool

	keyID             string
	issuerID          string
	privateKeyContent []byte

	token       *jwt.Token
	signedToken string

	client  HTTPClient
	BaseURL *url.URL

	common       service // Reuse a single struct instead of allocating one for each service on the heap.
	Provisioning *ProvisioningService
}

// NewClient creates a new client
func NewClient(httpClient HTTPClient, keyID, issuerID string, privateKey []byte) *Client {
	baseURL, err := url.Parse(baseURL)
	if err != nil {
		panic("invalid api base url: " + err.Error())
	}

	c := &Client{
		keyID:             keyID,
		issuerID:          issuerID,
		privateKeyContent: privateKey,

		client:  httpClient,
		BaseURL: baseURL,
	}
	c.common.client = c
	c.Provisioning = (*ProvisioningService)(&c.common)

	return c
}

// ensureSignedToken makes sure that the JWT auth token is not expired
// and return a signed key
func (c *Client) ensureSignedToken() (string, error) {
	if c.token != nil {
		claim, ok := c.token.Claims.(claims)
		if !ok {
			return "", fmt.Errorf("failed to cast claim for token")
		}
		expiration := time.Unix(int64(claim.Expiration), 0)

		// You do not need to generate a new token for every API request.
		// To get better performance from the App Store Connect API,
		// reuse the same signed token for up to 20 minutes.
		//  https://developer.apple.com/documentation/appstoreconnectapi/generating_tokens_for_api_requests
		if expiration.After(time.Now().Add(20 * time.Minute)) {
			return c.signedToken, nil
		}
	}

	c.token = createToken(c.keyID, c.issuerID)
	var err error
	if c.signedToken, err = signToken(c.token, c.privateKeyContent); err != nil {
		return "", err
	}
	return c.signedToken, nil
}

// NewRequest creates a new http.Request
func (c *Client) NewRequest(method, endpoint string, body interface{}) (*http.Request, error) {
	endpoint = apiVersion + "/" + endpoint
	u, err := c.BaseURL.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint failed: %v", err)
	}

	var buf io.ReadWriter
	if body != nil {
		buf = new(bytes.Buffer)
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return nil, fmt.Errorf("encoding body failed: %v", err)
		}
	}

	req, err := http.NewRequest(method, u.String(), buf)
	if err != nil {
		return nil, fmt.Errorf("preparing request failed: %v", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if _, ok := c.client.(*http.Client); ok {
		signedToken, err := c.ensureSignedToken()
		if err != nil {
			return nil, fmt.Errorf("ensuring JWT token failed: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+signedToken)
	}

	return req, nil
}

func checkResponse(r *http.Response) error {
	if r.StatusCode >= 200 && r.StatusCode <= 299 {
		return nil
	}

	errorResponse := &ErrorResponse{Response: r}
	data, err := ioutil.ReadAll(r.Body)
	if err == nil && data != nil {
		if err := json.Unmarshal(data, errorResponse); err != nil {
			log.Errorf("Failed to unmarshal response (%s): %s", string(data), err)
		}
	}
	return errorResponse
}

// Debugf ...
func (c *Client) Debugf(format string, v ...interface{}) {
	if c.EnableDebugLogs {
		log.Debugf(format, v...)
	}
}

// Do ...
func (c *Client) Do(req *http.Request, v interface{}) (*http.Response, error) {
	c.Debugf("Request:")
	if c.EnableDebugLogs {
		if err := httputil.PrintRequest(req); err != nil {
			c.Debugf("Failed to print request: %s", err)
		}
	}

	resp, err := c.client.Do(req)

	c.Debugf("Response:")
	if c.EnableDebugLogs {
		if err := httputil.PrintResponse(resp); err != nil {
			c.Debugf("Failed to print response: %s", err)
		}
	}

	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Warnf("Failed to close response body: %s", cerr)
		}
	}()

	if err := checkResponse(resp); err != nil {
		return resp, err
	}

	if v != nil {
		decErr := json.NewDecoder(resp.Body).Decode(v)
		if decErr == io.EOF {
			decErr = nil // ignore EOF errors caused by empty response body
		}
		if decErr != nil {
			err = decErr
		}
	}

	return resp, err
}

// PagingOptions ...
type PagingOptions struct {
	Limit  int    `url:"limit,omitempty"`
	Cursor string `url:"cursor,omitempty"`
	Next   string `url:"-"`
}

// UpdateCursor ...
func (opt *PagingOptions) UpdateCursor() error {
	if opt != nil && opt.Next != "" {
		u, err := url.Parse(opt.Next)
		if err != nil {
			return err
		}
		cursor := u.Query().Get("cursor")
		opt.Cursor = cursor
	}
	return nil
}

// addOptions adds the parameters in opt as URL query parameters to s. opt
// must be a struct whose fields may contain "url" tags.
func addOptions(s string, opt interface{}) (string, error) {
	v := reflect.ValueOf(opt)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return s, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return s, err
	}

	qs, err := query.Values(opt)
	if err != nil {
		return s, err
	}

	u.RawQuery = qs.Encode()
	return u.String(), nil
}
