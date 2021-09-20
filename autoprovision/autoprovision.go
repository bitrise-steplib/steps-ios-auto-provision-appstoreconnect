package autoprovision

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/keychain"
)

// APICertificate is certificate present on Apple App Store Connect API, could match a local certificate
type APICertificate struct {
	Certificate certificateutil.CertificateInfoModel
	ID          string
}

// CertificateSource ...
type CertificateSource interface {
	QueryCertificateBySerial(*big.Int) (APICertificate, error)
	QueryAllIOSCertificates() (map[appstoreconnect.CertificateType][]APICertificate, error)
}

// DevportalClient ...
type DevportalClient struct {
	CertificateSource CertificateSource
	DeviceClient      DeviceClient
	ProfileClient     ProfileClient
}

// NewAPIDevportalClient ...
func NewAPIDevportalClient(client *appstoreconnect.Client) DevportalClient {
	return DevportalClient{
		CertificateSource: NewAPICertificateSource(client),
		DeviceClient:      NewAPIDeviceClient(client),
		ProfileClient:     NewAPIProfileClient(client),
	}
}

type ProjectSettings struct {
	ProjectPath, Scheme, Configuration string
	SignUITestTargets                  bool
}

type CodesignRequirements struct {
	TeamID                                 string
	Platform                               Platform
	ArchivableTargetBundleIDToEntitlements map[string]serialized.Object
	UITestTargetBundleIDs                  []string
}

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

func SelectCertificatesAndDistributionTypes(certificateSource CertificateSource, certs []certificateutil.CertificateInfoModel, distribution DistributionType, teamID string, signUITestTargets bool, verboseLog bool) (map[appstoreconnect.CertificateType][]APICertificate, []DistributionType, error) {
	certType, ok := CertificateTypeByDistribution[distribution]
	if !ok {
		panic(fmt.Sprintf("no valid certificate provided for distribution type: %s", distribution))
	}

	distrTypes := []DistributionType{distribution}
	requiredCertTypes := map[appstoreconnect.CertificateType]bool{certType: true}
	if distribution != Development {
		distrTypes = append(distrTypes, Development)

		if signUITestTargets {
			log.Warnf("UITest target requires development code signing in addition to the specified %s code signing", distribution)
			requiredCertTypes[appstoreconnect.IOSDevelopment] = true
		} else {
			requiredCertTypes[appstoreconnect.IOSDevelopment] = false
		}
	}

	certsByType, err := GetValidCertificates(certs, certificateSource, requiredCertTypes, teamID, verboseLog)
	if err != nil {
		if missingCertErr, ok := err.(MissingCertificateError); ok {
			log.Errorf(err.Error())
			log.Warnf("Maybe you forgot to provide a(n) %s type certificate.", missingCertErr.Type)
			log.Warnf("Upload a %s type certificate (.p12) on the Code Signing tab of the Workflow Editor.", missingCertErr.Type)

			return nil, nil, fmt.Errorf("") // Move out
		}
		return nil, nil, fmt.Errorf("failed to get valid certificates: %s", err)
	}

	if len(certsByType) == 1 && distribution != Development {
		// remove development distribution if there is no development certificate uploaded
		distrTypes = []DistributionType{distribution}
	}
	log.Printf("ensuring codesigning files for distribution types: %s", distrTypes)

	return certsByType, distrTypes, nil
}

func EnsureTestDevices(deviceClient DeviceClient, testDevices []devportalservice.TestDevice, platform Platform) ([]string, error) {
	var devPortalDeviceIDs []string

	log.Infof("Fetching Apple Developer Portal devices")
	// IOS device platform includes: APPLE_WATCH, IPAD, IPHONE, IPOD and APPLE_TV device classes.
	devPortalDevices, err := deviceClient.ListDevices("", appstoreconnect.IOSDevice)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch devices: %s", err)
	}

	log.Printf("%d devices are registered on the Apple Developer Portal", len(devPortalDevices))
	for _, devPortalDevice := range devPortalDevices {
		log.Debugf("- %s, %s, UDID (%s), ID (%s)", devPortalDevice.Attributes.Name, devPortalDevice.Attributes.DeviceClass, devPortalDevice.Attributes.UDID, devPortalDevice.ID)
	}

	if len(testDevices) != 0 {
		fmt.Println()
		log.Infof("Checking if %d Bitrise test device(s) are registered on Developer Portal", len(testDevices))
		for _, d := range testDevices {
			log.Debugf("- %s, %s, UDID (%s), added at %s", d.Title, d.DeviceType, d.DeviceID, d.UpdatedAt)
		}

		newDevPortalDevices, err := registerMissingTestDevices(deviceClient, testDevices, devPortalDevices)
		if err != nil {
			return nil, fmt.Errorf("failed to register Bitrise Test device on Apple Developer Portal: %s", err)
		}
		devPortalDevices = append(devPortalDevices, newDevPortalDevices...)
	}

	devPortalDevices = filterDevPortalDevices(devPortalDevices, platform)

	for _, devPortalDevice := range devPortalDevices {
		devPortalDeviceIDs = append(devPortalDeviceIDs, devPortalDevice.ID)
	}

	return devPortalDeviceIDs, nil
}

type CodesignSettings struct {
	ArchivableTargetProfilesByBundleID map[string]Profile
	UITestTargetProfilesByBundleID     map[string]Profile
	Certificate                        certificateutil.CertificateInfoModel
}

func EnsureProfiles(profileClient ProfileClient, distrTypes []DistributionType,
	certsByType map[appstoreconnect.CertificateType][]APICertificate, requirements CodesignRequirements,
	devPortalDeviceIDs []string, minProfileDaysValid int) (map[DistributionType]CodesignSettings, error) {
	// Ensure Profiles
	codesignSettingsByDistributionType := map[DistributionType]CodesignSettings{}

	bundleIDByBundleIDIdentifer := map[string]*appstoreconnect.BundleID{}

	containersByBundleID := map[string][]string{}

	profileManager := ProfileManager{
		client:                      profileClient,
		bundleIDByBundleIDIdentifer: bundleIDByBundleIDIdentifer,
		containersByBundleID:        containersByBundleID,
	}

	for _, distrType := range distrTypes {
		fmt.Println()
		log.Infof("Checking %s provisioning profiles", distrType)
		certType := CertificateTypeByDistribution[distrType]
		certs := certsByType[certType]

		if len(certs) == 0 {
			return nil, fmt.Errorf("no valid certificate provided for distribution type: %s", distrType)
		} else if len(certs) > 1 {
			log.Warnf("Multiple certificates provided for distribution type: %s", distrType)
			for _, c := range certs {
				log.Warnf("- %s", c.Certificate.CommonName)
			}
			log.Warnf("Using: %s", certs[0].Certificate.CommonName)
		}
		log.Debugf("Using certificate for distribution type %s (certificate type %s): %s", distrType, certType, certs[0])

		codesignSettings := CodesignSettings{
			ArchivableTargetProfilesByBundleID: map[string]Profile{},
			UITestTargetProfilesByBundleID:     map[string]Profile{},
			Certificate:                        certs[0].Certificate,
		}

		var certIDs []string
		for _, cert := range certs {
			certIDs = append(certIDs, cert.ID)
		}

		platformProfileTypes, ok := PlatformToProfileTypeByDistribution[requirements.Platform]
		if !ok {
			return nil, fmt.Errorf("no profiles for platform: %s", requirements.Platform)
		}

		profileType := platformProfileTypes[distrType]

		for bundleIDIdentifier, entitlements := range requirements.ArchivableTargetBundleIDToEntitlements {
			var profileDeviceIDs []string
			if DistributionTypeRequiresDeviceList([]DistributionType{distrType}) {
				profileDeviceIDs = devPortalDeviceIDs
			}

			profile, err := profileManager.EnsureProfile(profileType, bundleIDIdentifier, entitlements, certIDs, profileDeviceIDs, minProfileDaysValid)
			if err != nil {
				return nil, err
			}
			codesignSettings.ArchivableTargetProfilesByBundleID[bundleIDIdentifier] = *profile

		}

		if len(requirements.UITestTargetBundleIDs) > 0 && distrType == Development {
			// Capabilities are not supported for UITest targets.
			// Xcode managed signing uses Wildcard Provisioning Profiles for UITest target signing.
			for _, bundleIDIdentifier := range requirements.UITestTargetBundleIDs {
				wildcardBundleID, err := createWildcardBundleID(bundleIDIdentifier)
				if err != nil {
					return nil, fmt.Errorf("could not create wildcard bundle id: %s", err)
				}

				// Capabilities are not supported for UITest targets.
				profile, err := profileManager.EnsureProfile(profileType, wildcardBundleID, nil, certIDs, devPortalDeviceIDs, minProfileDaysValid)
				if err != nil {
					return nil, err
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
		// TODO: improve error handling
		return nil, errors.New("you have to manually add the listed containers to your app ID at: https://developer.apple.com/account/resources/identifiers/list")
	}

	return codesignSettingsByDistributionType, nil
}

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

func InstallCertificatesAndProfiles(codesignSettingsByDistributionType map[DistributionType]CodesignSettings, keychainPath string, keychainPassword stepconf.Secret) error {
	fmt.Println()
	log.Infof("Install certificates and profiles")

	kc, err := keychain.New(keychainPath, keychainPassword)
	if err != nil {
		return fmt.Errorf("failed to initialize keychain: %s", err)
	}

	i := 0
	for _, codesignSettings := range codesignSettingsByDistributionType {
		log.Printf("certificate: %s", codesignSettings.Certificate.CommonName)

		if err := kc.InstallCertificate(codesignSettings.Certificate, ""); err != nil {
			return fmt.Errorf("failed to install certificate: %s", err)
		}

		log.Printf("profiles:")
		for _, profile := range codesignSettings.ArchivableTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := WriteProfile(profile); err != nil {
				return fmt.Errorf("failed to write profile to file: %s", err)
			}
		}

		for _, profile := range codesignSettings.UITestTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := WriteProfile(profile); err != nil {
				return fmt.Errorf("failed to write profile to file: %s", err)
			}
		}

		if i < len(codesignSettingsByDistributionType)-1 {
			fmt.Println()
		}
		i++
	}

	return nil
}
