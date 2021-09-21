package autocodesign

import (
	"fmt"
	"net/http"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnectclient"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/devportal"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/spaceship"
)

const notConnected = `Bitrise Apple service connection not found.
Most likely because there is no configured Bitrise Apple service connection.
Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/`

// CertificateFileURL contains a p12 file URL and passphrase
type CertificateFileURL struct {
	URL, Passphrase string
}

// Do ...
func Do(buildURL, buildAPIToken string,
	authSources []appleauth.Source, certificateURLs []CertificateFileURL, distributionType autoprovision.DistributionType,
	signUITestTargets, verboseLog bool,
	codesignRequirements autoprovision.CodesignRequirements, minProfileDaysValid int,
	keychainPath string, keychainPassword stepconf.Secret) (map[autoprovision.DistributionType]autoprovision.CodesignSettings, error) {

	fmt.Println()
	log.Infof("Fetching Apple service connection")
	connectionProvider := devportalservice.NewBitriseClient(retry.NewHTTPClient().StandardClient(), buildURL, buildAPIToken)
	conn, err := connectionProvider.GetAppleDeveloperConnection()
	if err != nil {
		if networkErr, ok := err.(devportalservice.NetworkError); ok && networkErr.Status == http.StatusUnauthorized {
			fmt.Println()
			log.Warnf("Unauthorized to query Bitrise Apple service connection. This happens by design, with a public app's PR build, to protect secrets.")
			return nil, err
		}

		fmt.Println()
		log.Errorf("Failed to activate Bitrise Apple service connection")
		log.Warnf("Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/")

		return nil, err
	}

	if len(conn.DuplicatedTestDevices) != 0 {
		log.Debugf("Devices with duplicated UDID are registered on Bitrise, will be ignored:")
		for _, d := range conn.DuplicatedTestDevices {
			log.Debugf("- %s, %s, UDID (%s), added at %s", d.Title, d.DeviceType, d.DeviceID, d.UpdatedAt)
		}
	}

	authConfig, err := appleauth.Select(conn, authSources, appleauth.Inputs{})
	if err != nil {
		if conn.APIKeyConnection == nil && conn.AppleIDConnection == nil {
			fmt.Println()
			log.Warnf("%s", notConnected)
		}
		return nil, fmt.Errorf("could not configure Apple service authentication: %v", err)
	}

	if authConfig.APIKey != nil {
		log.Donef("Using Apple service connection with API key.")
	} else if authConfig.AppleID != nil {
		log.Donef("Using Apple service connection with Apple ID.")
	} else {
		panic("No Apple authentication credentials found.")
	}

	// create developer portal client
	fmt.Println()
	log.Infof("Initializing Developer Portal client")
	var devportalClient devportal.Client
	if authConfig.APIKey != nil {
		httpClient := appstoreconnect.NewRetryableHTTPClient()
		client := appstoreconnect.NewClient(httpClient, authConfig.APIKey.KeyID, authConfig.APIKey.IssuerID, []byte(authConfig.APIKey.PrivateKey))
		client.EnableDebugLogs = false // Turn off client debug logs including HTTP call debug logs
		devportalClient = appstoreconnectclient.NewAPIDevportalClient(client)
		log.Donef("App Store Connect API client created with base URL: %s", client.BaseURL)
	} else if authConfig.AppleID != nil {
		client, err := spaceship.NewClient(*authConfig.AppleID, codesignRequirements.TeamID)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Apple ID client: %v", err)
		}
		devportalClient = spaceship.NewSpaceshipDevportalClient(client)
		log.Donef("Apple ID client created")
	}

	// Downloading certificates
	fmt.Println()
	log.Infof("Downloading certificates")

	certs, err := downloadCertificates(certificateURLs)
	if err != nil {
		return nil, fmt.Errorf("Failed to download certificates: %s", err)
	}

	log.Printf("%d certificates downloaded:", len(certs))

	for _, cert := range certs {
		log.Printf("- %s", cert.CommonName)
	}

	certsByType, distrTypes, err := autoprovision.SelectCertificatesAndDistributionTypes(
		devportalClient.CertificateSource,
		certs,
		distributionType,
		codesignRequirements.TeamID,
		signUITestTargets,
		verboseLog,
	)
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}

	// Ensure devices
	var devPortalDeviceIDs []string
	if autoprovision.DistributionTypeRequiresDeviceList(distrTypes) {
		var err error
		devPortalDeviceIDs, err = autoprovision.EnsureTestDevices(devportalClient.DeviceClient, conn.TestDevices, codesignRequirements.Platform)
		if err != nil {
			return nil, fmt.Errorf("Failed to ensure test devices: %s", err)
		}
	}

	// Ensure Profiles
	codesignSettingsByDistributionType, err := autoprovision.EnsureProfiles(devportalClient.ProfileClient, distrTypes, certsByType, codesignRequirements, devPortalDeviceIDs, minProfileDaysValid)
	if err != nil {
		return nil, fmt.Errorf("Failed to ensure profiles: %s", err)
	}

	// Install certificates and profiles
	if err := autoprovision.InstallCertificatesAndProfiles(codesignSettingsByDistributionType, keychainPath, keychainPassword); err != nil {
		return nil, fmt.Errorf("Failed to install codesigning files: %s", err)
	}

	return codesignSettingsByDistributionType, nil
}
