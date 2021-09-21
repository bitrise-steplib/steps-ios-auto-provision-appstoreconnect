package autocodesign

import (
	"fmt"
	"net/http"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnectclient"
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

// ProjectSettings ...
type ProjectSettings struct {
	ProjectPath, Scheme, Configuration string
	SignUITestTargets                  bool
}

// CodesignRequirements ...
type CodesignRequirements struct {
	TeamID                                 string
	Platform                               Platform
	ArchivableTargetBundleIDToEntitlements map[string]serialized.Object
	UITestTargetBundleIDs                  []string
}

// CodesignSettings ...
type CodesignSettings struct {
	ArchivableTargetProfilesByBundleID map[string]devportal.Profile
	UITestTargetProfilesByBundleID     map[string]devportal.Profile
	Certificate                        certificateutil.CertificateInfoModel
}

// Do ...
func Do(buildURL, buildAPIToken string,
	authSources []appleauth.Source, certificateURLs []CertificateFileURL, distributionType DistributionType,
	signUITestTargets, verboseLog bool,
	codesignRequirements CodesignRequirements, minProfileDaysValid int,
	keychainPath string, keychainPassword stepconf.Secret) (map[DistributionType]CodesignSettings, error) {

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

	certsByType, distrTypes, err := selectCertificatesAndDistributionTypes(
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
	if distributionTypeRequiresDeviceList(distrTypes) {
		var err error
		devPortalDeviceIDs, err = ensureTestDevices(devportalClient.DeviceClient, conn.TestDevices, codesignRequirements.Platform)
		if err != nil {
			return nil, fmt.Errorf("Failed to ensure test devices: %s", err)
		}
	}

	// Ensure Profiles
	codesignSettingsByDistributionType, err := ensureProfiles(devportalClient.ProfileClient, distrTypes, certsByType, codesignRequirements, devPortalDeviceIDs, minProfileDaysValid)
	if err != nil {
		return nil, fmt.Errorf("Failed to ensure profiles: %s", err)
	}

	// Install certificates and profiles
	if err := InstallCertificatesAndProfiles(codesignSettingsByDistributionType, keychainPath, keychainPassword); err != nil {
		return nil, fmt.Errorf("Failed to install codesigning files: %s", err)
	}

	return codesignSettingsByDistributionType, nil
}

// GetCodesignSettingsFromProject ...
func GetCodesignSettingsFromProject(settings ProjectSettings) (CodesignRequirements, string, error) {
	fmt.Println()
	log.Infof("Analyzing project")

	projHelper, config, err := NewProjectHelper(settings.ProjectPath, settings.Scheme, settings.Configuration)
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("failed to analyze project: %s", err)
	}

	log.Printf("Configuration: %s", config)

	teamID, err := projHelper.ProjectTeamID(config)
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("failed to read project team ID: %s", err)
	}

	log.Printf("Project team ID: %s", teamID)

	platform, err := projHelper.Platform(config)
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("Failed to read project platform: %s", err)
	}

	log.Printf("Platform: %s", platform)

	log.Printf("Application and App Extension targets:")
	for _, target := range projHelper.ArchivableTargets() {
		log.Printf("- %s", target.Name)
	}

	archivableTargetBundleIDToEntitlements, err := projHelper.ArchivableTargetBundleIDToEntitlements()
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("failed to read archivable targets' entitlements: %s", err)
	}

	if ok, entitlement, bundleID := CanGenerateProfileWithEntitlements(archivableTargetBundleIDToEntitlements); !ok {
		log.Errorf("Can not create profile with unsupported entitlement (%s) for the bundle ID %s, due to App Store Connect API limitations.", entitlement, bundleID)
		return CodesignRequirements{}, "", fmt.Errorf("please generate provisioning profile manually on Apple Developer Portal and use the Certificate and profile installer Step instead")
	}

	var uiTestTargetBundleIDs []string
	if settings.SignUITestTargets {
		log.Printf("UITest targets:")
		for _, target := range projHelper.UITestTargets {
			log.Printf("- %s", target.Name)
		}

		uiTestTargetBundleIDs, err = projHelper.UITestTargetBundleIDs()
		if err != nil {
			return CodesignRequirements{}, "", fmt.Errorf("Failed to read UITest targets' entitlements: %s", err)
		}
	}

	return CodesignRequirements{
		TeamID:                                 teamID,
		Platform:                               platform,
		ArchivableTargetBundleIDToEntitlements: archivableTargetBundleIDToEntitlements,
		UITestTargetBundleIDs:                  uiTestTargetBundleIDs,
	}, config, nil
}

// ForceCodesignSettings ...
func ForceCodesignSettings(projectSettings ProjectSettings, distribution DistributionType, codesignSettingsByDistributionType map[DistributionType]CodesignSettings) error {
	projHelper, config, err := NewProjectHelper(projectSettings.ProjectPath, projectSettings.Scheme, projectSettings.Configuration)
	if err != nil {
		return fmt.Errorf("failed to analyze project: %s", err)
	}

	fmt.Println()
	log.Infof("Apply Bitrise managed codesigning on the executable targets")
	for _, target := range projHelper.ArchivableTargets() {
		fmt.Println()
		log.Infof("  Target: %s", target.Name)

		forceCodesignDistribution := distribution
		if _, isDevelopmentAvailable := codesignSettingsByDistributionType[Development]; isDevelopmentAvailable {
			forceCodesignDistribution = Development
		}

		codesignSettings, ok := codesignSettingsByDistributionType[forceCodesignDistribution]
		if !ok {
			return fmt.Errorf("no codesign settings ensured for distribution type %s", forceCodesignDistribution)
		}
		teamID := codesignSettings.Certificate.TeamID

		targetBundleID, err := projHelper.TargetBundleID(target.Name, config)
		if err != nil {
			return err
		}
		profile, ok := codesignSettings.ArchivableTargetProfilesByBundleID[targetBundleID]
		if !ok {
			return fmt.Errorf("no profile ensured for the bundleID %s", targetBundleID)
		}

		log.Printf("  development Team: %s(%s)", codesignSettings.Certificate.TeamName, teamID)
		log.Printf("  provisioning Profile: %s", profile.Attributes().Name)
		log.Printf("  certificate: %s", codesignSettings.Certificate.CommonName)

		if err := projHelper.XcProj.ForceCodeSign(config, target.Name, teamID, codesignSettings.Certificate.CommonName, profile.Attributes().UUID); err != nil {
			return fmt.Errorf("failed to apply code sign settings for target (%s): %s", target.Name, err)
		}
	}

	devCodesignSettings, isDevelopmentAvailable := codesignSettingsByDistributionType[Development]
	if isDevelopmentAvailable && len(devCodesignSettings.UITestTargetProfilesByBundleID) != 0 {
		fmt.Println()
		log.Infof("Apply Bitrise managed codesigning on the UITest targets")
		for _, uiTestTarget := range projHelper.UITestTargets {
			fmt.Println()
			log.Infof("  Target: %s", uiTestTarget.Name)

			teamID := devCodesignSettings.Certificate.TeamID

			targetBundleID, err := projHelper.TargetBundleID(uiTestTarget.Name, config)
			if err != nil {
				return err
			}
			profile, ok := devCodesignSettings.UITestTargetProfilesByBundleID[targetBundleID]
			if !ok {
				return fmt.Errorf("no profile ensured for the bundleID %s", targetBundleID)
			}

			log.Printf("  development Team: %s(%s)", devCodesignSettings.Certificate.TeamName, teamID)
			log.Printf("  provisioning Profile: %s", profile.Attributes().Name)
			log.Printf("  certificate: %s", devCodesignSettings.Certificate.CommonName)

			for _, c := range uiTestTarget.BuildConfigurationList.BuildConfigurations {
				if err := projHelper.XcProj.ForceCodeSign(c.Name, uiTestTarget.Name, teamID, devCodesignSettings.Certificate.CommonName, profile.Attributes().UUID); err != nil {
					return fmt.Errorf("failed to apply code sign settings for target (%s): %s", uiTestTarget.Name, err)
				}
			}
		}
	}

	if err := projHelper.XcProj.Save(); err != nil {
		return fmt.Errorf("failed to save project: %s", err)
	}

	return nil
}
