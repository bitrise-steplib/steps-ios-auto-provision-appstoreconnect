package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/keychain"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/spaceship"
)

// downloadCertificates downloads and parses a list of p12 files
func downloadCertificates(URLs []CertificateFileURL) ([]certificateutil.CertificateInfoModel, error) {
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}
	var certInfos []certificateutil.CertificateInfoModel

	for i, p12 := range URLs {
		log.Debugf("Downloading p12 file number %d from %s", i, p12.URL)

		p12CertInfos, err := downloadPKCS12(httpClient, p12.URL, p12.Passphrase)
		if err != nil {
			return nil, err
		}
		log.Debugf("Codesign identities included:\n%s", autoprovision.CertsToString(p12CertInfos))

		certInfos = append(certInfos, p12CertInfos...)
	}

	return certInfos, nil
}

// downloadPKCS12 downloads a pkcs12 format file and parses certificates and matching private keys.
func downloadPKCS12(httpClient *http.Client, certificateURL, passphrase string) ([]certificateutil.CertificateInfoModel, error) {
	contents, err := downloadFile(httpClient, certificateURL)
	if err != nil {
		return nil, err
	} else if contents == nil {
		return nil, fmt.Errorf("certificate (%s) is empty", certificateURL)
	}

	infos, err := certificateutil.CertificatesFromPKCS12Content(contents, passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate (%s), err: %s", certificateURL, err)
	}

	return infos, nil
}

func downloadFile(httpClient *http.Client, src string) ([]byte, error) {
	url, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url (%s): %s", src, err)
	}

	// Local file
	if url.Scheme == "file" {
		src := strings.Replace(src, url.Scheme+"://", "", -1)

		return ioutil.ReadFile(src)
	}

	// Remote file
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %s", err)
	}

	var contents []byte
	err = retry.Times(2).Wait(5 * time.Second).Try(func(attempt uint) error {
		log.Debugf("Downloading %s, attempt %d", src, attempt)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download (%s): %s", src, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Warnf("failed to close (%s) body: %s", src, err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download (%s) failed with status code (%d)", src, resp.StatusCode)
		}

		contents, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response (%s): %s", src, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return contents, nil
}

func failf(format string, args ...interface{}) {
	log.Errorf(format, args...)
	os.Exit(1)
}

// ProfileManager ...
type ProfileManager struct {
	client                      autoprovision.ProfileClient
	bundleIDByBundleIDIdentifer map[string]*appstoreconnect.BundleID
	containersByBundleID        map[string][]string
}

// EnsureBundleID ...
func (m ProfileManager) EnsureBundleID(bundleIDIdentifier string, entitlements serialized.Object) (*appstoreconnect.BundleID, error) {
	fmt.Println()
	log.Infof("  Searching for app ID for bundle ID: %s", bundleIDIdentifier)

	bundleID, ok := m.bundleIDByBundleIDIdentifer[bundleIDIdentifier]
	if !ok {
		var err error
		bundleID, err = m.client.FindBundleID(bundleIDIdentifier)
		if err != nil {
			return nil, fmt.Errorf("failed to find bundle ID: %s", err)
		}
	}

	if bundleID != nil {
		log.Printf("  app ID found: %s", bundleID.Attributes.Name)

		m.bundleIDByBundleIDIdentifer[bundleIDIdentifier] = bundleID

		// Check if BundleID is sync with the project
		err := m.client.CheckBundleIDEntitlements(*bundleID, autoprovision.Entitlement(entitlements))
		if err != nil {
			if mErr, ok := err.(autoprovision.NonmatchingProfileError); ok {
				log.Warnf("  app ID capabilities invalid: %s", mErr.Reason)
				log.Warnf("  app ID capabilities are not in sync with the project capabilities, synchronizing...")
				if err := m.client.SyncBundleID(*bundleID, autoprovision.Entitlement(entitlements)); err != nil {
					return nil, fmt.Errorf("failed to update bundle ID capabilities: %s", err)
				}

				return bundleID, nil
			}

			return nil, fmt.Errorf("failed to validate bundle ID: %s", err)
		}

		log.Printf("  app ID capabilities are in sync with the project capabilities")

		return bundleID, nil
	}

	// Create BundleID
	log.Warnf("  app ID not found, generating...")

	capabilities := autoprovision.Entitlement(entitlements)

	bundleID, err := m.client.CreateBundleID(bundleIDIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to create bundle ID: %s", err)
	}

	containers, err := capabilities.ICloudContainers()
	if err != nil {
		return nil, fmt.Errorf("failed to get list of iCloud containers: %s", err)
	}

	if len(containers) > 0 {
		m.containersByBundleID[bundleIDIdentifier] = containers
		log.Errorf("  app ID created but couldn't add iCloud containers: %v", containers)
	}

	if err := m.client.SyncBundleID(*bundleID, capabilities); err != nil {
		return nil, fmt.Errorf("failed to update bundle ID capabilities: %s", err)
	}

	m.bundleIDByBundleIDIdentifer[bundleIDIdentifier] = bundleID

	return bundleID, nil
}

// EnsureProfile ...
func (m ProfileManager) EnsureProfile(profileType appstoreconnect.ProfileType, bundleIDIdentifier string, entitlements serialized.Object, certIDs, deviceIDs []string, minProfileDaysValid int) (*autoprovision.Profile, error) {
	fmt.Println()
	log.Infof("  Checking bundle id: %s", bundleIDIdentifier)
	log.Printf("  capabilities: %s", entitlements)

	// Search for Bitrise managed Profile
	name, err := autoprovision.ProfileName(profileType, bundleIDIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile name: %s", err)
	}

	profile, err := m.client.FindProfile(name, profileType)
	if err != nil {
		return nil, fmt.Errorf("failed to find profile: %s", err)
	}

	if profile == nil {
		log.Warnf("  profile does not exist, generating...")
	} else {
		log.Printf("  Bitrise managed profile found: %s ID: %s UUID: %s Expiry: %s", profile.Attributes().Name, profile.ID(), profile.Attributes().UUID, time.Time(profile.Attributes().ExpirationDate))

		if profile.Attributes().ProfileState == appstoreconnect.Active {
			// Check if Bitrise managed Profile is sync with the project
			err := autoprovision.CheckProfile(m.client, profile, autoprovision.Entitlement(entitlements), deviceIDs, certIDs, minProfileDaysValid)
			if err != nil {
				if mErr, ok := err.(autoprovision.NonmatchingProfileError); ok {
					log.Warnf("  the profile is not in sync with the project requirements (%s), regenerating ...", mErr.Reason)
				} else {
					return nil, fmt.Errorf("failed to check if profile is valid: %s", err)
				}
			} else { // Profile matches
				log.Donef("  profile is in sync with the project requirements")
				return &profile, nil
			}
		}

		if profile.Attributes().ProfileState == appstoreconnect.Invalid {
			// If the profile's bundle id gets modified, the profile turns in Invalid state.
			log.Warnf("  the profile state is invalid, regenerating ...")
		}

		if err := m.client.DeleteProfile(profile.ID()); err != nil {
			return nil, fmt.Errorf("failed to delete profile: %s", err)
		}
	}

	// Search for BundleID
	bundleID, err := m.EnsureBundleID(bundleIDIdentifier, entitlements)
	if err != nil {
		return nil, err
	}

	// Create Bitrise managed Profile
	fmt.Println()
	log.Infof("  Creating profile for bundle id: %s", bundleID.Attributes.Name)

	profile, err = m.client.CreateProfile(name, profileType, *bundleID, certIDs, deviceIDs)
	if err != nil {
		// Expired profiles are not listed via profiles endpoint,
		// so we can not catch if the profile already exist but expired, before we attempt to create one with the managed profile name.
		// As a workaround we use the BundleID profiles relationship url to find and delete the expired profile.
		if isMultipleProfileErr(err) {
			log.Warnf("  Profile already exists, but expired, cleaning up...")
			if err := m.client.DeleteExpiredProfile(bundleID, name); err != nil {
				return nil, fmt.Errorf("expired profile cleanup failed: %s", err)
			}

			profile, err = m.client.CreateProfile(name, profileType, *bundleID, certIDs, deviceIDs)
			if err != nil {
				return nil, fmt.Errorf("failed to create profile: %s", err)
			}

			log.Donef("  profile created: %s", profile.Attributes().Name)

			return &profile, nil
		}

		return nil, fmt.Errorf("failed to create profile: %s", err)
	}

	log.Donef("  profile created: %s", profile.Attributes().Name)

	return &profile, nil
}

func isMultipleProfileErr(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "multiple profiles found with the name")
}

const notConnected = `Connected Apple Developer Portal Account not found.
Most likely because there is no Apple Developer Portal Account connected to the build.
Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/`

func handleSessionDataError(err error) {
	if err == nil {
		return
	}

	if networkErr, ok := err.(devportalservice.NetworkError); ok && networkErr.Status == http.StatusUnauthorized {
		fmt.Println()
		log.Warnf("%s", "Unauthorized to query Connected Apple Developer Portal Account. This happens by design, with a public app's PR build, to protect secrets.")

		return
	}

	fmt.Println()
	log.Errorf("Failed to activate Bitrise Apple Developer Portal connection: %s", err)
	log.Warnf("Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/")
}

func createWildcardBundleID(bundleID string) (string, error) {
	idx := strings.LastIndex(bundleID, ".")
	if idx == -1 {
		return "", fmt.Errorf("invalid bundle id (%s): does not contain *", bundleID)
	}

	return bundleID[:idx] + ".*", nil
}

func main() {
	var stepConf Config
	if err := stepconf.Parse(&stepConf); err != nil {
		failf("Config: %s", err)
	}
	stepconf.Print(stepConf)

	log.SetEnableDebugLog(stepConf.VerboseLog)

	// Creating AppstoreConnectAPI client
	fmt.Println()
	log.Infof("Creating AppstoreConnectAPI client")

	authInputs := appleauth.Inputs{
		APIIssuer:  stepConf.APIIssuer,
		APIKeyPath: string(stepConf.APIKeyPath),
		// Apple ID
		Username:            stepConf.AppleID,
		Password:            string(stepConf.Password),
		AppSpecificPassword: string(stepConf.AppSpecificPassword),
	}
	if err := authInputs.Validate(); err != nil {
		failf("Issue with authentication related inputs: %v", err)
	}

	authSources, err := parseAuthSources(stepConf.BitriseConnection)
	if err != nil {
		failf("Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", stepConf.BitriseConnection)
	}

	var connectionProvider *devportalservice.BitriseClient
	if stepConf.BuildURL != "" && stepConf.BuildAPIToken != "" {
		connectionProvider = devportalservice.NewBitriseClient(retry.NewHTTPClient().StandardClient(), stepConf.BuildURL, stepConf.BuildAPIToken)
	} else {
		fmt.Println()
		log.Warnf("Connected Apple Developer Portal Account not found. Step is not running on bitrise.io: BITRISE_BUILD_URL and BITRISE_BUILD_API_TOKEN envs are not set")
	}
	var conn *devportalservice.AppleDeveloperConnection
	if stepConf.BitriseConnection != "off" && connectionProvider != nil {
		var err error
		conn, err = connectionProvider.GetAppleDeveloperConnection()
		if err != nil {
			handleSessionDataError(err)
		}

		if conn != nil && (conn.APIKeyConnection == nil) {
			fmt.Println()
			log.Warnf("%s", notConnected)
		}
	}

	authConfig, err := appleauth.Select(conn, authSources, authInputs)
	if err != nil {
		failf("Could not configure Apple Service authentication: %v", err)
	}

	// Remove
	var client *appstoreconnect.Client

	var devportalClient autoprovision.DevportalClient
	if authConfig.APIKey != nil {
		httpClient := appstoreconnect.NewRetryableHTTPClient()
		client = appstoreconnect.NewClient(httpClient, authConfig.APIKey.KeyID, authConfig.APIKey.IssuerID, []byte(authConfig.APIKey.PrivateKey))
		client.EnableDebugLogs = false // Turn off client debug logs including HTTP call debug logs
		log.Donef("the client created for %s", client.BaseURL)
		devportalClient = autoprovision.NewAPIDevportalClient(client)
	} else {
		client, err := spaceship.NewClient(authConfig.AppleID, stepConf.TeamID)
		if err != nil {
			failf("failed to initialize Spaceship client: %v")
		}
		devportalClient = spaceship.NewSpaceshipDevportalClient(client)
	}

	// Analyzing project
	fmt.Println()
	log.Infof("Analyzing project")

	projHelper, config, err := autoprovision.NewProjectHelper(stepConf.ProjectPath, stepConf.Scheme, stepConf.Configuration)
	if err != nil {
		failf("Failed to analyze project: %s", err)
	}

	log.Printf("Configuration: %s", config)

	teamID, err := projHelper.ProjectTeamID(config)
	if err != nil {
		failf("Failed to read project team ID: %s", err)
	}

	log.Printf("Project team ID: %s", teamID)

	platform, err := projHelper.Platform(config)
	if err != nil {
		failf("Failed to read project platform: %s", err)
	}

	log.Printf("Platform: %s", platform)

	log.Printf("Application and App Extension targets:")
	for _, target := range projHelper.ArchivableTargets() {
		log.Printf("- %s", target.Name)
	}
	if stepConf.SignUITestTargets {
		log.Printf("UITest targets:")
		for _, target := range projHelper.UITestTargets {
			log.Printf("- %s", target.Name)
		}
	}

	archivableTargetBundleIDToEntitlements, err := projHelper.ArchivableTargetBundleIDToEntitlements()
	if err != nil {
		failf("Failed to read archivable targets' entitlements: %s", err)
	}

	if ok, entitlement, bundleID := autoprovision.CanGenerateProfileWithEntitlements(archivableTargetBundleIDToEntitlements); !ok {
		log.Errorf("Can not create profile with unsupported entitlement (%s) for the bundle ID %s, due to App Store Connect API limitations.", entitlement, bundleID)
		failf("Please generate provisioning profile manually on Apple Developer Portal and use the Certificate and profile installer Step instead.")
	}

	uiTestTargetBundleIDs, err := projHelper.UITestTargetBundleIDs()
	if err != nil {
		failf("Failed to read UITest targets' entitlements: %s", err)
	}

	// Downloading certificates
	fmt.Println()
	log.Infof("Downloading certificates")

	certURLs, err := stepConf.CertificateFileURLs()
	if err != nil {
		failf("Failed to convert certificate URLs: %s", err)
	}

	certs, err := downloadCertificates(certURLs)
	if err != nil {
		failf("Failed to download certificates: %s", err)
	}

	log.Printf("%d certificates downloaded:", len(certs))

	for _, cert := range certs {
		log.Printf("- %s", cert.CommonName)
	}

	certType, ok := autoprovision.CertificateTypeByDistribution[stepConf.DistributionType()]
	if !ok {
		failf("No valid certificate provided for distribution type: %s", stepConf.DistributionType())
	}

	distrTypes := []autoprovision.DistributionType{stepConf.DistributionType()}
	requiredCertTypes := map[appstoreconnect.CertificateType]bool{certType: true}
	if stepConf.DistributionType() != autoprovision.Development {
		distrTypes = append(distrTypes, autoprovision.Development)

		if stepConf.SignUITestTargets {
			log.Warnf("UITest target requires development code signing in addition to the specified %s code signing", stepConf.DistributionType())
			requiredCertTypes[appstoreconnect.IOSDevelopment] = true
		} else {
			requiredCertTypes[appstoreconnect.IOSDevelopment] = false
		}
	}

	certsByType, err := autoprovision.GetValidCertificates(certs, devportalClient.CertificateSource, requiredCertTypes, teamID, stepConf.VerboseLog)
	if err != nil {
		if missingCertErr, ok := err.(autoprovision.MissingCertificateError); ok {
			log.Errorf(err.Error())
			log.Warnf("Maybe you forgot to provide a(n) %s type certificate.", missingCertErr.Type)
			log.Warnf("Upload a %s type certificate (.p12) on the Code Signing tab of the Workflow Editor.", missingCertErr.Type)
			os.Exit(1)
		}
		failf("Failed to get valid certificates: %s", err)
	}

	if len(certsByType) == 1 && stepConf.DistributionType() != autoprovision.Development {
		// remove development distribution if there is no development certificate uploaded
		distrTypes = []autoprovision.DistributionType{stepConf.DistributionType()}
	}
	log.Printf("ensuring codesigning files for distribution types: %s", distrTypes)

	// Ensure devices
	var devPortalDeviceIDs []string

	if distributionTypeRequiresDeviceList(distrTypes) {
		log.Infof("Fetching Apple Developer Portal devices")
		// IOS device platform includes: APPLE_WATCH, IPAD, IPHONE, IPOD and APPLE_TV device classes.
		devPortalDevices, err := devportalClient.DeviceClient.ListDevices("", appstoreconnect.IOSDevice)
		if err != nil {
			failf("Failed to fetch devices: %s", err)
		}

		log.Printf("%d devices are registered on the Apple Developer Portal", len(devPortalDevices))
		for _, devPortalDevice := range devPortalDevices {
			log.Debugf("- %s, %s, UDID (%s), ID (%s)", devPortalDevice.Attributes.Name, devPortalDevice.Attributes.DeviceClass, devPortalDevice.Attributes.UDID, devPortalDevice.ID)
		}

		if stepConf.RegisterTestDevices && conn != nil && len(conn.TestDevices) != 0 {
			fmt.Println()
			log.Infof("Checking if %d Bitrise test device(s) are registered on Developer Portal", len(conn.TestDevices))
			for _, d := range conn.TestDevices {
				log.Debugf("- %s, %s, UDID (%s), added at %s", d.Title, d.DeviceType, d.DeviceID, d.UpdatedAt)
			}

			if len(conn.DuplicatedTestDevices) != 0 {
				log.Warnf("Devices with duplicated UDID are registered on Bitrise, will be ignored:")
				for _, d := range conn.DuplicatedTestDevices {
					log.Warnf("- %s, %s, UDID (%s), added at %s", d.Title, d.DeviceType, d.DeviceID, d.UpdatedAt)
				}
			}

			newDevPortalDevices, err := registerMissingTestDevices(devportalClient.DeviceClient, conn.TestDevices, devPortalDevices)
			if err != nil {
				failf("Failed to register Bitrise Test device on Apple Developer Portal: %s", err)
			}
			devPortalDevices = append(devPortalDevices, newDevPortalDevices...)
		}

		devPortalDevices = filterDevPortalDevices(devPortalDevices, platform)

		for _, devPortalDevice := range devPortalDevices {
			devPortalDeviceIDs = append(devPortalDeviceIDs, devPortalDevice.ID)
		}
	}

	// Ensure Profiles
	type CodesignSettings struct {
		ArchivableTargetProfilesByBundleID map[string]autoprovision.Profile
		UITestTargetProfilesByBundleID     map[string]autoprovision.Profile
		Certificate                        certificateutil.CertificateInfoModel
	}

	codesignSettingsByDistributionType := map[autoprovision.DistributionType]CodesignSettings{}

	bundleIDByBundleIDIdentifer := map[string]*appstoreconnect.BundleID{}

	containersByBundleID := map[string][]string{}

	profileManager := ProfileManager{
		client:                      devportalClient.ProfileClient,
		bundleIDByBundleIDIdentifer: bundleIDByBundleIDIdentifer,
		containersByBundleID:        containersByBundleID,
	}

	for _, distrType := range distrTypes {
		fmt.Println()
		log.Infof("Checking %s provisioning profiles", distrType)
		certType := autoprovision.CertificateTypeByDistribution[distrType]
		certs := certsByType[certType]

		if len(certs) == 0 {
			failf("No valid certificate provided for distribution type: %s", distrType)
		} else if len(certs) > 1 {
			log.Warnf("Multiple certificates provided for distribution type: %s", distrType)
			for _, c := range certs {
				log.Warnf("- %s", c.Certificate.CommonName)
			}
			log.Warnf("Using: %s", certs[0].Certificate.CommonName)
		}
		log.Debugf("Using certificate for distribution type %s (certificate type %s): %s", distrType, certType, certs[0])

		codesignSettings := CodesignSettings{
			ArchivableTargetProfilesByBundleID: map[string]autoprovision.Profile{},
			UITestTargetProfilesByBundleID:     map[string]autoprovision.Profile{},
			Certificate:                        certs[0].Certificate,
		}

		var certIDs []string
		for _, cert := range certs {
			certIDs = append(certIDs, cert.ID)
		}

		platformProfileTypes, ok := autoprovision.PlatformToProfileTypeByDistribution[platform]
		if !ok {
			failf("No profiles for platform: %s", platform)
		}

		profileType := platformProfileTypes[distrType]

		for bundleIDIdentifier, entitlements := range archivableTargetBundleIDToEntitlements {
			var profileDeviceIDs []string
			if distributionTypeRequiresDeviceList([]autoprovision.DistributionType{distrType}) {
				profileDeviceIDs = devPortalDeviceIDs
			}

			profile, err := profileManager.EnsureProfile(profileType, bundleIDIdentifier, entitlements, certIDs, profileDeviceIDs, stepConf.MinProfileDaysValid)
			if err != nil {
				failf(err.Error())
			}
			codesignSettings.ArchivableTargetProfilesByBundleID[bundleIDIdentifier] = *profile

		}

		if stepConf.SignUITestTargets && distrType == autoprovision.Development {
			// Capabilities are not supported for UITest targets.
			// Xcode managed signing uses Wildcard Provisioning Profiles for UITest target signing.
			for _, bundleIDIdentifier := range uiTestTargetBundleIDs {
				wildcardBundleID, err := createWildcardBundleID(bundleIDIdentifier)
				if err != nil {
					failf("Could not create wildcard bundle id: %s", err)
				}

				// Capabilities are not supported for UITest targets.
				profile, err := profileManager.EnsureProfile(profileType, wildcardBundleID, nil, certIDs, devPortalDeviceIDs, stepConf.MinProfileDaysValid)
				if err != nil {
					failf(err.Error())
				}
				codesignSettings.UITestTargetProfilesByBundleID[bundleIDIdentifier] = *profile
			}
		}

		codesignSettingsByDistributionType[distrType] = codesignSettings
	}

	if len(containersByBundleID) > 0 {
		fmt.Println()
		log.Errorf("Unable to automatically assign iCloud containers to the following app IDs:")
		fmt.Println()
		for bundleID, containers := range containersByBundleID {
			log.Warnf("%s, containers:", bundleID)
			for _, container := range containers {
				log.Warnf("- %s", container)
			}
			fmt.Println()
		}
		failf("You have to manually add the listed containers to your app ID at: https://developer.apple.com/account/resources/identifiers/list")
	}

	// Force Codesign Settings
	fmt.Println()
	log.Infof("Apply Bitrise managed codesigning on the executable targets")
	for _, target := range projHelper.ArchivableTargets() {
		fmt.Println()
		log.Infof("  Target: %s", target.Name)

		forceCodesignDistribution := stepConf.DistributionType()
		if _, isDevelopmentAvailable := codesignSettingsByDistributionType[autoprovision.Development]; isDevelopmentAvailable {
			forceCodesignDistribution = autoprovision.Development
		}

		codesignSettings, ok := codesignSettingsByDistributionType[forceCodesignDistribution]
		if !ok {
			failf("No codesign settings ensured for distribution type %s", stepConf.DistributionType())
		}
		teamID = codesignSettings.Certificate.TeamID

		targetBundleID, err := projHelper.TargetBundleID(target.Name, config)
		if err != nil {
			failf(err.Error())
		}
		profile, ok := codesignSettings.ArchivableTargetProfilesByBundleID[targetBundleID]
		if !ok {
			failf("No profile ensured for the bundleID %s", targetBundleID)
		}

		log.Printf("  development Team: %s(%s)", codesignSettings.Certificate.TeamName, teamID)
		log.Printf("  provisioning Profile: %s", profile.Attributes().Name)
		log.Printf("  certificate: %s", codesignSettings.Certificate.CommonName)

		if err := projHelper.XcProj.ForceCodeSign(config, target.Name, teamID, codesignSettings.Certificate.CommonName, profile.Attributes().UUID); err != nil {
			failf("Failed to apply code sign settings for target (%s): %s", target.Name, err)
		}
	}

	if stepConf.SignUITestTargets {
		fmt.Println()
		log.Infof("Apply Bitrise managed codesigning on the UITest targets")
		for _, uiTestTarget := range projHelper.UITestTargets {
			fmt.Println()
			log.Infof("  Target: %s", uiTestTarget.Name)

			forceCodesignDistribution := autoprovision.Development

			codesignSettings, ok := codesignSettingsByDistributionType[forceCodesignDistribution]
			if !ok {
				failf("No codesign settings ensured for distribution type %s", stepConf.DistributionType())
			}
			teamID = codesignSettings.Certificate.TeamID

			targetBundleID, err := projHelper.TargetBundleID(uiTestTarget.Name, config)
			if err != nil {
				failf(err.Error())
			}
			profile, ok := codesignSettings.UITestTargetProfilesByBundleID[targetBundleID]
			if !ok {
				failf("No profile ensured for the bundleID %s", targetBundleID)
			}

			log.Printf("  development Team: %s(%s)", codesignSettings.Certificate.TeamName, teamID)
			log.Printf("  provisioning Profile: %s", profile.Attributes().Name)
			log.Printf("  certificate: %s", codesignSettings.Certificate.CommonName)

			for _, c := range uiTestTarget.BuildConfigurationList.BuildConfigurations {
				if err := projHelper.XcProj.ForceCodeSign(c.Name, uiTestTarget.Name, teamID, codesignSettings.Certificate.CommonName, profile.Attributes().UUID); err != nil {
					failf("Failed to apply code sign settings for target (%s): %s", uiTestTarget.Name, err)
				}
			}
		}
	}

	if err := projHelper.XcProj.Save(); err != nil {
		failf("Failed to save project: %s", err)
	}

	// Install certificates and profiles
	fmt.Println()
	log.Infof("Install certificates and profiles")

	kc, err := keychain.New(stepConf.KeychainPath, stepConf.KeychainPassword)
	if err != nil {
		failf("Failed to initialize keychain: %s", err)
	}

	i := 0
	for _, codesignSettings := range codesignSettingsByDistributionType {
		log.Printf("certificate: %s", codesignSettings.Certificate.CommonName)

		if err := kc.InstallCertificate(codesignSettings.Certificate, ""); err != nil {
			failf("Failed to install certificate: %s", err)
		}

		log.Printf("profiles:")
		for _, profile := range codesignSettings.ArchivableTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := autoprovision.WriteProfile(profile); err != nil {
				failf("Failed to write profile to file: %s", err)
			}
		}

		for _, profile := range codesignSettings.UITestTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := autoprovision.WriteProfile(profile); err != nil {
				failf("Failed to write profile to file: %s", err)
			}
		}

		if i < len(codesignSettingsByDistributionType)-1 {
			fmt.Println()
		}
		i++
	}

	// Export output
	fmt.Println()
	log.Infof("Exporting outputs")

	outputs := map[string]string{
		"BITRISE_EXPORT_METHOD":  stepConf.Distribution,
		"BITRISE_DEVELOPER_TEAM": teamID,
	}

	settings, ok := codesignSettingsByDistributionType[autoprovision.Development]
	if ok {
		outputs["BITRISE_DEVELOPMENT_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := projHelper.TargetBundleID(projHelper.MainTarget.Name, config)
		if err != nil {
			failf("Failed to read bundle ID for the main target: %s", err)
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_DEVELOPMENT_PROFILE"] = profile.Attributes().UUID
	}

	if stepConf.DistributionType() != autoprovision.Development {
		settings, ok := codesignSettingsByDistributionType[stepConf.DistributionType()]
		if !ok {
			failf("No codesign settings ensured for the selected distribution type: %s", stepConf.DistributionType())
		}

		outputs["BITRISE_PRODUCTION_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := projHelper.TargetBundleID(projHelper.MainTarget.Name, config)
		if err != nil {
			failf(err.Error())
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_PRODUCTION_PROFILE"] = profile.Attributes().UUID
	}

	for k, v := range outputs {
		log.Donef("%s=%s", k, v)
		if err := tools.ExportEnvironmentWithEnvman(k, v); err != nil {
			failf("Failed to export %s=%s: %s", k, v, err)
		}
	}

}
