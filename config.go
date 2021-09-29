package main

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/sliceutil"
	"github.com/bitrise-io/go-xcode/autocodesign"
	"github.com/bitrise-io/go-xcode/autocodesign/certdownloader"
	"github.com/bitrise-io/go-xcode/autocodesign/devportalclient"
)

// Config holds the step inputs
type Config struct {
	BitriseConnection string          `env:"connection,opt[automatic,api_key,off,enterprise_with_apple_id,enterprise-with-apple-id,apple_id,apple-id]"`
	APIKeyPath        stepconf.Secret `env:"api_key_path"`
	APIIssuer         string          `env:"api_issuer"`
	// Apple ID
	TeamID string `env:"apple_id_team_id"`

	ProjectPath         string `env:"project_path,dir"`
	Scheme              string `env:"scheme,required"`
	Configuration       string `env:"configuration"`
	SignUITestTargets   bool   `env:"sign_uitest_targets,opt[yes,no]"`
	RegisterTestDevices bool   `env:"register_test_devices,opt[yes,no]"`

	Distribution        string `env:"distribution_type,opt[development,app-store,ad-hoc,enterprise]"`
	MinProfileDaysValid int    `env:"min_profile_days_valid"`

	CertificateURLList        string          `env:"certificate_urls,required"`
	CertificatePassphraseList stepconf.Secret `env:"passphrases"`
	KeychainPath              string          `env:"keychain_path,required"`
	KeychainPassword          stepconf.Secret `env:"keychain_password,required"`

	VerboseLog bool `env:"verbose_log,opt[no,yes]"`

	BuildAPIToken string `env:"build_api_token"`
	BuildURL      string `env:"build_url"`
}

// DistributionType ...
func (c Config) DistributionType() autocodesign.DistributionType {
	return autocodesign.DistributionType(c.Distribution)
}

// ValidateCertificates validates if the number of certificate URLs matches those of passphrases
func (c Config) ValidateCertificates() ([]string, []string, error) {
	pfxURLs := splitAndClean(c.CertificateURLList, "|", true)
	passphrases := splitAndClean(string(c.CertificatePassphraseList), "|", false)

	if len(pfxURLs) != len(passphrases) {
		return nil, nil, fmt.Errorf("certificates count (%d) and passphrases count (%d) should match", len(pfxURLs), len(passphrases))
	}

	return pfxURLs, passphrases, nil
}

// Certificates returns an array of p12 file URLs and passphrases
func (c Config) Certificates() ([]certdownloader.CertificateAndPassphrase, error) {
	pfxURLs, passphrases, err := c.ValidateCertificates()
	if err != nil {
		return nil, err
	}

	files := make([]certdownloader.CertificateAndPassphrase, len(pfxURLs))
	for i, pfxURL := range pfxURLs {
		files[i] = certdownloader.CertificateAndPassphrase{
			URL:        pfxURL,
			Passphrase: passphrases[i],
		}
	}

	return files, nil
}

// SplitAndClean ...
func splitAndClean(list string, sep string, omitEmpty bool) (items []string) {
	return sliceutil.CleanWhitespace(strings.Split(list, sep), omitEmpty)
}

func parseAuthSources(bitriseConnection string) (devportalclient.ClientType, error) {
	switch bitriseConnection {
	case "automatic":
		return devportalclient.APIKeyClient, nil
	case "api_key":
		return devportalclient.APIKeyClient, nil
	case "apple_id", "apple-id", "enterprise_with_apple_id", "enterprise-with-apple-id":
		return devportalclient.AppleIDClient, nil
	case "off":
		return devportalclient.APIKeyClient, nil
	default:
		return devportalclient.APIKeyClient, fmt.Errorf("invalid connection input: %s", bitriseConnection)
	}
}
