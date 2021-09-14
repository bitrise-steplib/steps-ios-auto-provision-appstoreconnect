package spaceship

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

// CertificateSource ...
type CertificateSource struct {
	client       *Client
	certificates map[appstoreconnect.CertificateType][]autoprovision.APICertificate
}

// QueryCertificateBySerial ...
func (s *CertificateSource) QueryCertificateBySerial(serial *big.Int) (autoprovision.APICertificate, error) {
	if s.certificates == nil {
		if err := s.downloadAll(); err != nil {
			return autoprovision.APICertificate{}, err
		}
	}

	allCerts := append(s.certificates[appstoreconnect.IOSDevelopment], s.certificates[appstoreconnect.IOSDistribution]...)
	for _, cert := range allCerts {
		if serial.Cmp(cert.Certificate.Certificate.SerialNumber) == 0 {
			return cert, nil
		}
	}

	return autoprovision.APICertificate{}, fmt.Errorf("can not find certificate with serial")
}

// QueryAllIOSCertificates ...
func (s *CertificateSource) QueryAllIOSCertificates() (map[appstoreconnect.CertificateType][]autoprovision.APICertificate, error) {
	if s.certificates == nil {
		if err := s.downloadAll(); err != nil {
			return nil, err
		}
	}

	return s.certificates, nil
}

// NewSpaceshipCertificateSource ...
func NewSpaceshipCertificateSource(client *Client) autoprovision.CertificateSource {
	return &CertificateSource{
		client: client,
	}
}

func (s *CertificateSource) downloadAll() error {
	devCertsCmd, err := s.client.createRequestCommand("list_dev_certs")
	if err != nil {
		return err
	}

	distCertsCommand, err := s.client.createRequestCommand("list_dist_certs")
	if err != nil {
		return err
	}

	devCerts, err := parseCertificates(devCertsCmd)
	if err != nil {
		return err
	}

	distCers, err := parseCertificates(distCertsCommand)
	if err != nil {
		return err
	}

	s.certificates = map[appstoreconnect.CertificateType][]autoprovision.APICertificate{
		appstoreconnect.IOSDevelopment:  devCerts,
		appstoreconnect.IOSDistribution: distCers,
	}

	return nil
}

type certificatesResponse struct {
	Data []struct {
		Content string `json:"content"`
		ID      string `json:"id"`
	} `json:"data"`
}

func parseCertificates(cmd spaceshipCommand) ([]autoprovision.APICertificate, error) {
	output, err := runSpaceshipCommand(cmd)
	if err != nil {
		return nil, err
	}

	var certificates certificatesResponse
	if err := json.Unmarshal([]byte(output), &certificates); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	var certInfos []autoprovision.APICertificate
	for _, certInfo := range certificates.Data {
		pemContent, err := base64.StdEncoding.DecodeString(certInfo.Content)
		if err != nil {
			return nil, err
		}

		cert, err := certificateutil.CeritifcateFromPemContent(pemContent)
		if err != nil {
			return nil, err
		}

		certInfos = append(certInfos, autoprovision.APICertificate{
			Certificate: certificateutil.NewCertificateInfo(*cert, nil),
			ID:          certInfo.ID,
		})
	}

	return certInfos, nil
}
