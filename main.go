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
	"github.com/bitrise-io/go-xcode/xcodeproject/xcodeproj"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/keychain"
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

func needToRegisterDevices(distrTypes []autoprovision.DistributionType) bool {
	for _, distrType := range distrTypes {
		if distrType == autoprovision.Development || distrType == autoprovision.AdHoc {
			return true
		}
	}
	return false
}

func keys(obj map[string]serialized.Object) (s []string) {
	for key := range obj {
		s = append(s, key)
	}
	return
}

func failf(format string, args ...interface{}) {
	log.Errorf(format, args...)
	os.Exit(1)
}

func registerMissingDevices(client *appstoreconnect.Client, bitriseDevices []devportalservice.TestDevice, devportalDevices []appstoreconnect.Device) ([]appstoreconnect.Device, error) {
	if client == nil {
		return []appstoreconnect.Device{}, fmt.Errorf("App Store Connect client not provided")
	}

	newDevices := []appstoreconnect.Device{}
	for _, testDevice := range bitriseDevices {
		log.Printf("checking if the device (%s) is registered", testDevice.DeviceID)

		found := false
		for _, device := range devportalDevices {
			if devportalservice.IsEqualUDID(device.Attributes.UDID, testDevice.DeviceID) {
				found = true
				break
			}
		}

		if found {
			log.Printf("device already registered")

			continue
		}

		// The API seems to recognize existing devices even with different casing and '-' separator removed.
		// The Developer Portal UI does not let adding devices with unexpected casing or separators removed.
		// Did not fully validate the ability to add devices with changed casing (or '-' removed) via the API, so passing the UDID through unchanged.
		log.Printf("registering device")
		req := appstoreconnect.DeviceCreateRequest{
			Data: appstoreconnect.DeviceCreateRequestData{
				Attributes: appstoreconnect.DeviceCreateRequestDataAttributes{
					Name:     "Bitrise test device",
					Platform: appstoreconnect.IOS,
					UDID:     testDevice.DeviceID,
				},
				Type: "devices",
			},
		}

		registeredDevice, err := client.Provisioning.RegisterNewDevice(req)
		if err != nil {
			rerr, ok := err.(*appstoreconnect.ErrorResponse)
			if ok && rerr.Response != nil && rerr.Response.StatusCode == http.StatusConflict {
				log.Warnf("Failed to register device (can be caused by invalid UDID or trying to register a Mac device): %s", err)

				continue
			}

			return []appstoreconnect.Device{}, fmt.Errorf("%v", err)
		}
		if registeredDevice != nil {
			newDevices = append(newDevices, registeredDevice.Data)
		}
	}

	return newDevices, nil
}

// ProfileManager ...
type ProfileManager struct {
	client                      *appstoreconnect.Client
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
		bundleID, err = autoprovision.FindBundleID(m.client, bundleIDIdentifier)
		if err != nil {
			return nil, fmt.Errorf("failed to find bundle ID: %s", err)
		}
	}

	if bundleID != nil {
		log.Printf("  app ID found: %s", bundleID.Attributes.Name)

		m.bundleIDByBundleIDIdentifer[bundleIDIdentifier] = bundleID

		// Check if BundleID is sync with the project
		err := autoprovision.CheckBundleIDEntitlements(m.client, *bundleID, autoprovision.Entitlement(entitlements))
		if err != nil {
			if mErr, ok := err.(autoprovision.NonmatchingProfileError); ok {
				log.Warnf("  app ID capabilities invalid: %s", mErr.Reason)
				log.Warnf("  app ID capabilities are not in sync with the project capabilities, synchronizing...")
				if err := autoprovision.SyncBundleID(m.client, bundleID.ID, autoprovision.Entitlement(entitlements)); err != nil {
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

	bundleID, err := autoprovision.CreateBundleID(m.client, bundleIDIdentifier)
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

	if err := autoprovision.SyncBundleID(m.client, bundleID.ID, capabilities); err != nil {
		return nil, fmt.Errorf("failed to update bundle ID capabilities: %s", err)
	}

	m.bundleIDByBundleIDIdentifer[bundleIDIdentifier] = bundleID

	return bundleID, nil
}

// EnsureProfile ...
func (m ProfileManager) EnsureProfile(profileType appstoreconnect.ProfileType, bundleIDIdentifier string, entitlements serialized.Object, certIDs, deviceIDs []string, minProfileDaysValid int) (*appstoreconnect.Profile, error) {
	fmt.Println()
	log.Infof("  Checking bundle id: %s", bundleIDIdentifier)
	log.Printf("  capabilities: %s", entitlements)

	// Search for Bitrise managed Profile
	name, err := autoprovision.ProfileName(profileType, bundleIDIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile name: %s", err)
	}

	profile, err := autoprovision.FindProfile(m.client, name, profileType, bundleIDIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to find profile: %s", err)
	}

	if profile == nil {
		log.Warnf("  profile does not exist, generating...")
	} else {
		log.Printf("  Bitrise managed profile found: %s", profile.Attributes.Name)

		if profile.Attributes.ProfileState == appstoreconnect.Active {
			// Check if Bitrise managed Profile is sync with the project
			err := autoprovision.CheckProfile(m.client, *profile, autoprovision.Entitlement(entitlements), deviceIDs, certIDs, minProfileDaysValid)
			if err != nil {
				if mErr, ok := err.(autoprovision.NonmatchingProfileError); ok {
					log.Warnf("  the profile is not in sync with the project requirements (%s), regenerating ...", mErr.Reason)
				} else {
					return nil, fmt.Errorf("failed to check if profile is valid: %s", err)
				}
			} else { // Profile matches
				log.Donef("  profile is in sync with the project requirements")
				return profile, nil
			}
		}

		if profile.Attributes.ProfileState == appstoreconnect.Invalid {
			// If the profile's bundle id gets modified, the profile turns in Invalid state.
			log.Warnf("  the profile state is invalid, regenerating ...")
		}

		if err := autoprovision.DeleteProfile(m.client, profile.ID); err != nil {
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

	profile, err = autoprovision.CreateProfile(m.client, name, profileType, *bundleID, certIDs, deviceIDs)
	if err != nil {
		// Expired profiles are not listed via profiles endpoint,
		// so we can not catch if the profile already exist but expired, before we attempt to create one with the managed profile name.
		// As a workaround we use the BundleID profiles relationship url to find and delete the expired profile.
		if isMultipleProfileErr(err) {
			log.Warnf("  Profile already exists, but expired, cleaning up...")
			if err := m.deleteExpiredProfile(bundleID, name); err != nil {
				return nil, fmt.Errorf("expired profile cleanup failed: %s", err)
			}

			profile, err = autoprovision.CreateProfile(m.client, name, profileType, *bundleID, certIDs, deviceIDs)
			if err != nil {
				return nil, fmt.Errorf("failed to create profile: %s", err)
			}

			log.Donef("  profile created: %s", profile.Attributes.Name)

			return profile, nil
		}

		return nil, fmt.Errorf("failed to create profile: %s", err)
	}

	log.Donef("  profile created: %s", profile.Attributes.Name)

	return profile, nil
}

func (m ProfileManager) deleteExpiredProfile(bundleID *appstoreconnect.BundleID, profileName string) error {
	var nextPageURL string
	var profile *appstoreconnect.Profile

	for {
		response, err := m.client.Provisioning.Profiles(bundleID.Relationships.Profiles.Links.Related, &appstoreconnect.PagingOptions{
			Limit: 20,
			Next:  nextPageURL,
		})
		if err != nil {
			return err
		}

		for _, d := range response.Data {
			if d.Attributes.Name == profileName {
				profile = &d
				break
			}
		}

		nextPageURL = response.Links.Next
		if nextPageURL == "" {
			break
		}
	}

	if profile == nil {
		return fmt.Errorf("failed to find profile: %s", profileName)
	}

	return m.client.Provisioning.DeleteProfile(profile.ID)
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
	}
	if err := authInputs.Validate(); err != nil {
		failf("Issue with authentication related inputs: %v", err)
	}

	authSources, err := parseAuthSources(stepConf.BitriseConnection)
	if err != nil {
		failf("Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", stepConf.BitriseConnection)
	}

	var devportalConnectionProvider *devportalservice.BitriseClient
	if stepConf.BuildURL != "" && stepConf.BuildAPIToken != "" {
		devportalConnectionProvider = devportalservice.NewBitriseClient(http.DefaultClient, stepConf.BuildURL, string(stepConf.BuildAPIToken))
	} else {
		fmt.Println()
		log.Warnf("Connected Apple Developer Portal Account not found. Step is not running on bitrise.io: BITRISE_BUILD_URL and BITRISE_BUILD_API_TOKEN envs are not set")
	}
	var conn *devportalservice.AppleDeveloperConnection
	if stepConf.BitriseConnection != "off" && devportalConnectionProvider != nil {
		var err error
		conn, err = devportalConnectionProvider.GetAppleDeveloperConnection()
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

	client := appstoreconnect.NewClient(http.DefaultClient, authConfig.APIKey.KeyID, authConfig.APIKey.IssuerID, []byte(authConfig.APIKey.PrivateKey))

	// Turn off client debug logs includeing HTTP call debug logs
	client.EnableDebugLogs = false

	log.Donef("the client created for %s", client.BaseURL)

	// Analyzing project
	fmt.Println()
	log.Infof("Analyzing project")

	projHelper, config, err := autoprovision.NewProjectHelper(stepConf.ProjectPath, stepConf.Scheme, stepConf.Configuration)
	if err != nil {
		failf("Failed to analyze project: %s", err)
	}

	log.Printf("configuration: %s", config)

	teamID, err := projHelper.ProjectTeamID(config)
	if err != nil {
		failf("Failed to read project team ID: %s", err)
	}

	log.Printf("project team ID: %s", teamID)

	entitlementsByBundleID, err := projHelper.ArchivableTargetBundleIDToEntitlements()
	if err != nil {
		failf("Failed to read bundle ID entitlements: %s", err)
	}

	log.Printf("bundle IDs:")
	for _, id := range keys(entitlementsByBundleID) {
		log.Printf("- %s", id)
	}

	if ok, entitlement, bundleID := autoprovision.CanGenerateProfileWithEntitlements(entitlementsByBundleID); !ok {
		log.Errorf("Can not create profile with unsupported entitlement (%s) for the bundle ID %s, due to App Store Connect API limitations.", entitlement, bundleID)
		failf("Please generate provisioning profile manually on Apple Developer Portal and use the Certificate and profile installer Step instead.")
	}

	platform, err := projHelper.Platform(config)
	if err != nil {
		failf("Failed to read project platform: %s", err)
	}

	log.Printf("platform: %s", platform)

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
		requiredCertTypes[appstoreconnect.IOSDevelopment] = false
	}

	certClient := autoprovision.APIClient(client)
	certsByType, err := autoprovision.GetValidCertificates(certs, certClient, requiredCertTypes, teamID, stepConf.VerboseLog)
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
	var devices []appstoreconnect.Device

	if needToRegisterDevices(distrTypes) {
		fmt.Println()
		log.Infof("Fetching test devices")

		var err error
		devices, err = autoprovision.ListDevices(client, "", appstoreconnect.IOSDevice)
		if err != nil {
			failf("Failed to list devices: %s", err)
		}

		log.Printf("%d devices are registered on Developer Portal", len(devices))
		for _, d := range devices {
			log.Debugf("- %s, %s, UDID (%s), ID (%s)", d.Attributes.Name, d.Attributes.DeviceClass, d.Attributes.UDID, d.ID)
		}

		if conn != nil && len(conn.TestDevices) != 0 {
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

			registeredDevices, err := registerMissingDevices(client, conn.TestDevices, devices)
			if err != nil {
				failf("Failed to add devices registered on Bitrise to Developer Portal: %s", err)
			}
			devices = append(devices, registeredDevices...)
		}
	}

	// Ensure Profiles
	type CodesignSettings struct {
		ProfilesByBundleID map[string]appstoreconnect.Profile
		Certificate        certificateutil.CertificateInfoModel
	}

	codesignSettingsByDistributionType := map[autoprovision.DistributionType]CodesignSettings{}

	bundleIDByBundleIDIdentifer := map[string]*appstoreconnect.BundleID{}

	containersByBundleID := map[string][]string{}

	profileManager := ProfileManager{
		client:                      client,
		bundleIDByBundleIDIdentifer: bundleIDByBundleIDIdentifer,
		containersByBundleID:        containersByBundleID,
	}

	for _, distrType := range distrTypes {
		fmt.Println()
		log.Infof("Checking %s provisioning profiles for %d bundle id(s)", distrType, len(entitlementsByBundleID))
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
			ProfilesByBundleID: map[string]appstoreconnect.Profile{},
			Certificate:        certs[0].Certificate,
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

		var deviceIDs []string
		if needToRegisterDevices([]autoprovision.DistributionType{distrType}) {
			for _, d := range devices {
				if strings.HasPrefix(string(profileType), "TVOS") && d.Attributes.DeviceClass != "APPLE_TV" {
					log.Debugf("dropping device %s, since device type: %s, required device type: APPLE_TV", d.ID, d.Attributes.DeviceClass)
					continue
				} else if strings.HasPrefix(string(profileType), "IOS") &&
					string(d.Attributes.DeviceClass) != "IPHONE" && string(d.Attributes.DeviceClass) != "IPAD" && string(d.Attributes.DeviceClass) != "IPOD" {
					log.Debugf("dropping device %s, since device type: %s, required device type: IPHONE, IPAD or IPOD", d.ID, d.Attributes.DeviceClass)
					continue
				}
				deviceIDs = append(deviceIDs, d.ID)
			}
		}

		for bundleIDIdentifier, entitlements := range entitlementsByBundleID {
			profile, err := profileManager.EnsureProfile(profileType, bundleIDIdentifier, entitlements, certIDs, deviceIDs, stepConf.MinProfileDaysValid)
			if err != nil {
				failf(err.Error())
			}
			codesignSettings.ProfilesByBundleID[bundleIDIdentifier] = *profile
			codesignSettingsByDistributionType[distrType] = codesignSettings
		}
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
	log.Infof("Apply Bitrise managed codesigning on the project")

	targets := append([]xcodeproj.Target{projHelper.MainTarget}, projHelper.MainTarget.DependentExecutableProductTargets(false)...)
	for _, target := range targets {
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
		profile, ok := codesignSettings.ProfilesByBundleID[targetBundleID]
		if !ok {
			failf("No profile ensured for the bundleID %s", targetBundleID)
		}

		log.Printf("  development Team: %s(%s)", codesignSettings.Certificate.TeamName, teamID)
		log.Printf("  provisioning Profile: %s", profile.Attributes.Name)
		log.Printf("  certificate: %s", codesignSettings.Certificate.CommonName)

		if err := projHelper.XcProj.ForceCodeSign(config, target.Name, teamID, codesignSettings.Certificate.CommonName, profile.Attributes.UUID); err != nil {
			failf("Failed to apply code sign settings for target (%s): %s", target.Name, err)
		}

		if err := projHelper.XcProj.Save(); err != nil {
			failf("Failed to save project: %s", err)
		}

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
		for _, profile := range codesignSettings.ProfilesByBundleID {
			log.Printf("- %s", profile.Attributes.Name)

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
		profile, ok := settings.ProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_DEVELOPMENT_PROFILE"] = profile.Attributes.UUID
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
		profile, ok := settings.ProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_PRODUCTION_PROFILE"] = profile.Attributes.UUID
	}

	for k, v := range outputs {
		log.Donef("%s=%s", k, v)
		if err := tools.ExportEnvironmentWithEnvman(k, v); err != nil {
			failf("Failed to export %s=%s: %s", k, v, err)
		}
	}

}
