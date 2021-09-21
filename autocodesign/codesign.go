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

// AppLayout ...
type AppLayout struct {
	TeamID                                 string
	Platform                               Platform
	ArchivableTargetBundleIDToEntitlements map[string]serialized.Object
	UITestTargetBundleIDs                  []string
}

// AppCodesignAssets ...
type AppCodesignAssets struct {
	ArchivableTargetProfilesByBundleID map[string]devportal.Profile
	UITestTargetProfilesByBundleID     map[string]devportal.Profile
	Certificate                        certificateutil.CertificateInfoModel
}

// Manager ...
type Manager struct {
	devportalClient devportal.Client
	testDevices     []devportalservice.TestDevice
}

// NewManager ...
func NewManager(buildURL, buildAPIToken string, authSources []appleauth.Source, authInputs appleauth.Inputs, teamID string) (Manager, error) {
	fmt.Println()
	log.Infof("Fetching Apple service connection")
	connectionProvider := devportalservice.NewBitriseClient(retry.NewHTTPClient().StandardClient(), buildURL, buildAPIToken)
	conn, err := connectionProvider.GetAppleDeveloperConnection()
	if err != nil {
		if networkErr, ok := err.(devportalservice.NetworkError); ok && networkErr.Status == http.StatusUnauthorized {
			fmt.Println()
			log.Warnf("Unauthorized to query Bitrise Apple service connection. This happens by design, with a public app's PR build, to protect secrets.")
			return Manager{}, err
		}

		fmt.Println()
		log.Errorf("Failed to activate Bitrise Apple service connection")
		log.Warnf("Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/")

		return Manager{}, err
	}

	if len(conn.DuplicatedTestDevices) != 0 {
		log.Debugf("Devices with duplicated UDID are registered on Bitrise, will be ignored:")
		for _, d := range conn.DuplicatedTestDevices {
			log.Debugf("- %s, %s, UDID (%s), added at %s", d.Title, d.DeviceType, d.DeviceID, d.UpdatedAt)
		}
	}

	authConfig, err := appleauth.Select(conn, authSources, authInputs)
	if err != nil {
		if conn.APIKeyConnection == nil && conn.AppleIDConnection == nil {
			fmt.Println()
			log.Warnf("%s", notConnected)
		}
		return Manager{}, fmt.Errorf("could not configure Apple service authentication: %v", err)
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
		client, err := spaceship.NewClient(*authConfig.AppleID, teamID)
		if err != nil {
			return Manager{}, fmt.Errorf("failed to initialize Apple ID client: %v", err)
		}
		devportalClient = spaceship.NewSpaceshipDevportalClient(client)
		log.Donef("Apple ID client created")
	}

	return Manager{
		devportalClient: devportalClient,
		testDevices:     conn.TestDevices,
	}, nil
}

// KeychainCredentials ...
type KeychainCredentials struct {
	Path     string
	Password stepconf.Secret
}

// AutoCodesign ...
func (m Manager) AutoCodesign(
	distributionType DistributionType,
	project AppLayout,
	certificateURLs []CertificateFileURL,
	minProfileDaysValid int,
	keychain KeychainCredentials,
	verboseLog bool,
) (map[DistributionType]AppCodesignAssets, error) {

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

	signUITestTargets := len(project.UITestTargetBundleIDs) > 0
	certsByType, distrTypes, err := selectCertificatesAndDistributionTypes(
		m.devportalClient.CertificateSource,
		certs,
		distributionType,
		project.TeamID,
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
		devPortalDeviceIDs, err = ensureTestDevices(m.devportalClient.DeviceClient, m.testDevices, project.Platform)
		if err != nil {
			return nil, fmt.Errorf("Failed to ensure test devices: %s", err)
		}
	}

	// Ensure Profiles
	codesignAssetsByDistributionType, err := ensureProfiles(m.devportalClient.ProfileClient, distrTypes, certsByType, project, devPortalDeviceIDs, minProfileDaysValid)
	if err != nil {
		return nil, fmt.Errorf("Failed to ensure profiles: %s", err)
	}

	// Install certificates and profiles
	if err := installCodesigningFiles(codesignAssetsByDistributionType, keychain); err != nil {
		return nil, fmt.Errorf("Failed to install codesigning files: %s", err)
	}

	return codesignAssetsByDistributionType, nil
}

// Project ...
type Project struct {
	projHelper ProjectHelper
}

// NewProject ...
func NewProject(projOrWSPath, schemeName, configurationName string) (Project, error) {
	projectHelper, err := NewProjectHelper(projOrWSPath, schemeName, configurationName)
	if err != nil {
		return Project{}, err
	}

	return Project{
		projHelper: *projectHelper,
	}, nil
}

// MainTargetBundleID ...
func (p Project) MainTargetBundleID() (string, error) {
	bundleID, err := p.projHelper.TargetBundleID(p.projHelper.MainTarget.Name, p.projHelper.Configuration)
	if err != nil {
		return "", fmt.Errorf("failed to read bundle ID for the main target: %s", err)
	}

	return bundleID, nil
}

// GetAppLayout ...
func (p Project) GetAppLayout(uiTestTargets bool) (AppLayout, error) {
	log.Printf("Configuration: %s", p.projHelper.Configuration)

	teamID, err := p.projHelper.ProjectTeamID(p.projHelper.Configuration)
	if err != nil {
		return AppLayout{}, fmt.Errorf("failed to read project team ID: %s", err)
	}

	log.Printf("Project team ID: %s", teamID)

	platform, err := p.projHelper.Platform(p.projHelper.Configuration)
	if err != nil {
		return AppLayout{}, fmt.Errorf("Failed to read project platform: %s", err)
	}

	log.Printf("Platform: %s", platform)

	log.Printf("Application and App Extension targets:")
	for _, target := range p.projHelper.ArchivableTargets() {
		log.Printf("- %s", target.Name)
	}

	archivableTargetBundleIDToEntitlements, err := p.projHelper.ArchivableTargetBundleIDToEntitlements()
	if err != nil {
		return AppLayout{}, fmt.Errorf("failed to read archivable targets' entitlements: %s", err)
	}

	if ok, entitlement, bundleID := CanGenerateProfileWithEntitlements(archivableTargetBundleIDToEntitlements); !ok {
		log.Errorf("Can not create profile with unsupported entitlement (%s) for the bundle ID %s, due to App Store Connect API limitations.", entitlement, bundleID)
		return AppLayout{}, fmt.Errorf("please generate provisioning profile manually on Apple Developer Portal and use the Certificate and profile installer Step instead")
	}

	var uiTestTargetBundleIDs []string
	if uiTestTargets {
		log.Printf("UITest targets:")
		for _, target := range p.projHelper.UITestTargets {
			log.Printf("- %s", target.Name)
		}

		uiTestTargetBundleIDs, err = p.projHelper.UITestTargetBundleIDs()
		if err != nil {
			return AppLayout{}, fmt.Errorf("Failed to read UITest targets' entitlements: %s", err)
		}
	}

	return AppLayout{
		TeamID:                                 teamID,
		Platform:                               platform,
		ArchivableTargetBundleIDToEntitlements: archivableTargetBundleIDToEntitlements,
		UITestTargetBundleIDs:                  uiTestTargetBundleIDs,
	}, nil
}

// ForceCodesignAssets ...
func (p Project) ForceCodesignAssets(distribution DistributionType, codesignAssetsByDistributionType map[DistributionType]AppCodesignAssets) error {
	fmt.Println()
	log.Infof("Apply Bitrise managed codesigning on the executable targets")
	for _, target := range p.projHelper.ArchivableTargets() {
		fmt.Println()
		log.Infof("  Target: %s", target.Name)

		forceCodesignDistribution := distribution
		if _, isDevelopmentAvailable := codesignAssetsByDistributionType[Development]; isDevelopmentAvailable {
			forceCodesignDistribution = Development
		}

		codesignAssets, ok := codesignAssetsByDistributionType[forceCodesignDistribution]
		if !ok {
			return fmt.Errorf("no codesign settings ensured for distribution type %s", forceCodesignDistribution)
		}
		teamID := codesignAssets.Certificate.TeamID

		targetBundleID, err := p.projHelper.TargetBundleID(target.Name, p.projHelper.Configuration)
		if err != nil {
			return err
		}
		profile, ok := codesignAssets.ArchivableTargetProfilesByBundleID[targetBundleID]
		if !ok {
			return fmt.Errorf("no profile ensured for the bundleID %s", targetBundleID)
		}

		log.Printf("  development Team: %s(%s)", codesignAssets.Certificate.TeamName, teamID)
		log.Printf("  provisioning Profile: %s", profile.Attributes().Name)
		log.Printf("  certificate: %s", codesignAssets.Certificate.CommonName)

		if err := p.projHelper.XcProj.ForceCodeSign(p.projHelper.Configuration, target.Name, teamID, codesignAssets.Certificate.CommonName, profile.Attributes().UUID); err != nil {
			return fmt.Errorf("failed to apply code sign settings for target (%s): %s", target.Name, err)
		}
	}

	devCodesignAssets, isDevelopmentAvailable := codesignAssetsByDistributionType[Development]
	if isDevelopmentAvailable && len(devCodesignAssets.UITestTargetProfilesByBundleID) != 0 {
		fmt.Println()
		log.Infof("Apply Bitrise managed codesigning on the UITest targets")
		for _, uiTestTarget := range p.projHelper.UITestTargets {
			fmt.Println()
			log.Infof("  Target: %s", uiTestTarget.Name)

			teamID := devCodesignAssets.Certificate.TeamID

			targetBundleID, err := p.projHelper.TargetBundleID(uiTestTarget.Name, p.projHelper.Configuration)
			if err != nil {
				return err
			}
			profile, ok := devCodesignAssets.UITestTargetProfilesByBundleID[targetBundleID]
			if !ok {
				return fmt.Errorf("no profile ensured for the bundleID %s", targetBundleID)
			}

			log.Printf("  development Team: %s(%s)", devCodesignAssets.Certificate.TeamName, teamID)
			log.Printf("  provisioning Profile: %s", profile.Attributes().Name)
			log.Printf("  certificate: %s", devCodesignAssets.Certificate.CommonName)

			for _, c := range uiTestTarget.BuildConfigurationList.BuildConfigurations {
				if err := p.projHelper.XcProj.ForceCodeSign(c.Name, uiTestTarget.Name, teamID, devCodesignAssets.Certificate.CommonName, profile.Attributes().UUID); err != nil {
					return fmt.Errorf("failed to apply code sign settings for target (%s): %s", uiTestTarget.Name, err)
				}
			}
		}
	}

	if err := p.projHelper.XcProj.Save(); err != nil {
		return fmt.Errorf("failed to save project: %s", err)
	}

	return nil
}
