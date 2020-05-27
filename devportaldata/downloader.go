package devportaldata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/retry"
)

// Downloader downloads/reads the developer portal json
type Downloader struct {
	BuildAPIToken string
	BuildURL      string

	ReadBytesFromFile func(pth string) ([]byte, error)
	DownloadContent   func(url string, buildAPIToken string) ([]byte, error)
}

// NewDownloader creates a new Downloader instance
func NewDownloader(buildURL string, buildAPIToken string) *Downloader {
	return &Downloader{
		BuildAPIToken:     buildAPIToken,
		BuildURL:          buildURL,
		DownloadContent:   downloadContent,
		ReadBytesFromFile: fileutil.ReadBytesFromFile,
	}
}

func (c Downloader) parseDevPortalData(data []byte) (*DevPortalData, error) {
	var devPortalData DevPortalData
	if err := json.Unmarshal(data, &devPortalData); err != nil {
		return nil, err
	}

	if devPortalData.IssuerID == "" {
		return nil, errors.New("invalid App Store Connect API authentication data: missing issuer_id")
	}
	if devPortalData.KeyID == "" {
		return nil, errors.New("invalid App Store Connect API authentication data: missing key_id")
	}
	if devPortalData.PrivateKey == "" {
		return nil, errors.New("invalid App Store Connect API authentication data: missing private_key")
	}

	return &devPortalData, nil
}

// GetDevPortalData ...
func (c Downloader) GetDevPortalData() (*DevPortalData, error) {
	var data []byte
	var err error

	if strings.HasPrefix(c.BuildURL, "file://") {
		data, err = c.ReadBytesFromFile(strings.TrimPrefix(c.BuildURL, "file://"))
	} else {
		var u *url.URL
		u, err = url.Parse(c.BuildURL)
		if err != nil {
			return nil, err
		}
		u.Path = path.Join(u.Path, "apple_developer_portal_data.json")
		data, err = c.DownloadContent(u.String(), c.BuildAPIToken)
	}

	if err != nil {
		return nil, err
	}

	return c.parseDevPortalData(data)
}

func downloadContent(url string, buildAPIToken string) ([]byte, error) {
	var contentBytes []byte
	return contentBytes, retry.Times(2).Wait(time.Duration(3) * time.Second).Try(func(attempt uint) error {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request for url %s: %s", url, err)
		}

		if buildAPIToken != "" {
			req.Header.Add("BUILD_API_TOKEN", buildAPIToken)
		}

		client := http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download from %s: %s", url, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Warnf("failed to close (%s) body", url)
			}
		}()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("request failed with status HTTP%d", resp.StatusCode)
		}

		contentBytes, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read received conent: %s", err)
		}
		return nil
	})
}
