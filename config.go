package main

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/sliceutil"
	"github.com/bitrise-steplib/steps-deploy-to-itunesconnect-deliver/appleauth"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

// CertificateFileURL contains a p12 file URL and passphrase
type CertificateFileURL struct {
	URL, Passphrase string
}

// Config holds the step inputs
type Config struct {
	BitriseConnection string `env:"connection,opt[automatic,api_key,off]"`
	APIKeyPath        stepconf.Secret `env:"api_key_path"`
	APIIssuer         string `env:"api_issuer"`

	ProjectPath   string `env:"project_path,dir"`
	Scheme        string `env:"scheme,required"`
	Configuration string `env:"configuration"`

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
func (c Config) DistributionType() autoprovision.DistributionType {
	return autoprovision.DistributionType(c.Distribution)
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

// CertificateFileURLs returns an array of p12 file URLs and passphrases
func (c Config) CertificateFileURLs() ([]CertificateFileURL, error) {
	pfxURLs, passphrases, err := c.ValidateCertificates()
	if err != nil {
		return nil, err
	}

	files := make([]CertificateFileURL, len(pfxURLs))
	for i, pfxURL := range pfxURLs {
		files[i] = CertificateFileURL{
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

func parseAuthSources(bitriseConnection string) ([]appleauth.Source, error) {
	switch bitriseConnection {
	case "automatic":
		return []appleauth.Source{
			&appleauth.ConnectionAPIKeySource{},
			&appleauth.InputAPIKeySource{},
		}, nil
	case "api_key":
		return []appleauth.Source{
			&appleauth.ConnectionAPIKeySource{},
		}, nil
	case "off":
		return []appleauth.Source{
			&appleauth.InputAPIKeySource{},
		}, nil
	default:
		return nil, fmt.Errorf("invalid connection input: %s", bitriseConnection)
	}
}
