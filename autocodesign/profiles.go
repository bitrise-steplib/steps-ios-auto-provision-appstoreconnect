package autocodesign

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/devportal"
)

func ensureProfiles(profileClient devportal.ProfileClient, distrTypes []DistributionType,
	certsByType map[appstoreconnect.CertificateType][]devportal.Certificate, requirements CodesignRequirements,
	devPortalDeviceIDs []string, minProfileDaysValid int) (map[DistributionType]CodesignSettings, error) {
	// Ensure Profiles
	codesignSettingsByDistributionType := map[DistributionType]CodesignSettings{}

	bundleIDByBundleIDIdentifer := map[string]*appstoreconnect.BundleID{}

	containersByBundleID := map[string][]string{}

	profileManager := profileManager{
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
			ArchivableTargetProfilesByBundleID: map[string]devportal.Profile{},
			UITestTargetProfilesByBundleID:     map[string]devportal.Profile{},
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
			if distributionTypeRequiresDeviceList([]DistributionType{distrType}) {
				profileDeviceIDs = devPortalDeviceIDs
			}

			profile, err := profileManager.ensureProfile(profileType, bundleIDIdentifier, entitlements, certIDs, profileDeviceIDs, minProfileDaysValid)
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
				profile, err := profileManager.ensureProfile(profileType, wildcardBundleID, nil, certIDs, devPortalDeviceIDs, minProfileDaysValid)
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

type profileManager struct {
	client                      devportal.ProfileClient
	bundleIDByBundleIDIdentifer map[string]*appstoreconnect.BundleID
	containersByBundleID        map[string][]string
}

func (m profileManager) ensureBundleID(bundleIDIdentifier string, entitlements serialized.Object) (*appstoreconnect.BundleID, error) {
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
		err := m.client.CheckBundleIDEntitlements(*bundleID, devportal.Entitlement(entitlements))
		if err != nil {
			if mErr, ok := err.(devportal.NonmatchingProfileError); ok {
				log.Warnf("  app ID capabilities invalid: %s", mErr.Reason)
				log.Warnf("  app ID capabilities are not in sync with the project capabilities, synchronizing...")
				if err := m.client.SyncBundleID(*bundleID, devportal.Entitlement(entitlements)); err != nil {
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

	capabilities := devportal.Entitlement(entitlements)

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

func (m profileManager) ensureProfile(profileType appstoreconnect.ProfileType, bundleIDIdentifier string, entitlements serialized.Object, certIDs, deviceIDs []string, minProfileDaysValid int) (*devportal.Profile, error) {
	fmt.Println()
	log.Infof("  Checking bundle id: %s", bundleIDIdentifier)
	log.Printf("  capabilities: %s", entitlements)

	// Search for Bitrise managed Profile
	name, err := profileName(profileType, bundleIDIdentifier)
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
			err := checkProfile(m.client, profile, devportal.Entitlement(entitlements), deviceIDs, certIDs, minProfileDaysValid)
			if err != nil {
				if mErr, ok := err.(devportal.NonmatchingProfileError); ok {
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
	bundleID, err := m.ensureBundleID(bundleIDIdentifier, entitlements)
	if err != nil {
		return nil, err
	}

	// Create Bitrise managed Profile
	fmt.Println()
	log.Infof("  Creating profile for bundle id: %s", bundleID.Attributes.Name)

	profile, err = m.client.CreateProfile(name, profileType, *bundleID, certIDs, deviceIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile: %s", err)
	}

	log.Donef("  profile created: %s", profile.Attributes().Name)
	return &profile, nil
}

func distributionTypeRequiresDeviceList(distrTypes []DistributionType) bool {
	for _, distrType := range distrTypes {
		if distrType == Development || distrType == AdHoc {
			return true
		}
	}
	return false
}

func createWildcardBundleID(bundleID string) (string, error) {
	idx := strings.LastIndex(bundleID, ".")
	if idx == -1 {
		return "", fmt.Errorf("invalid bundle id (%s): does not contain *", bundleID)
	}

	return bundleID[:idx] + ".*", nil
}

// profileName generates profile name with layout: Bitrise <platform> <distribution type> - (<bundle id>)
func profileName(profileType appstoreconnect.ProfileType, bundleID string) (string, error) {
	platform, ok := ProfileTypeToPlatform[profileType]
	if !ok {
		return "", fmt.Errorf("unknown profile type: %s", profileType)
	}

	distribution, ok := ProfileTypeToDistribution[profileType]
	if !ok {
		return "", fmt.Errorf("unknown profile type: %s", profileType)
	}

	prefix := ""
	if strings.HasSuffix(bundleID, ".*") {
		// `*` char is not allowed in Profile name.
		bundleID = strings.TrimSuffix(bundleID, ".*")
		prefix = "Wildcard "
	}

	return fmt.Sprintf("%sBitrise %s %s - (%s)", prefix, platform, distribution, bundleID), nil
}

func checkProfileEntitlements(client devportal.ProfileClient, prof devportal.Profile, projectEntitlements devportal.Entitlement) error {
	profileEnts, err := parseRawProfileEntitlements(prof)
	if err != nil {
		return err
	}

	projectEnts := serialized.Object(projectEntitlements)

	missingContainers, err := findMissingContainers(projectEnts, profileEnts)
	if err != nil {
		return fmt.Errorf("failed to check missing containers: %s", err)
	}
	if len(missingContainers) > 0 {
		return devportal.NonmatchingProfileError{
			Reason: fmt.Sprintf("project uses containers that are missing from the provisioning profile: %v", missingContainers),
		}
	}

	bundleID, err := prof.BundleID()
	if err != nil {
		return err
	}

	return client.CheckBundleIDEntitlements(bundleID, projectEntitlements)
}

func parseRawProfileEntitlements(prof devportal.Profile) (serialized.Object, error) {
	pkcs, err := profileutil.ProvisioningProfileFromContent(prof.Attributes().ProfileContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pkcs7 from profile content: %s", err)
	}

	profile, err := profileutil.NewProvisioningProfileInfo(*pkcs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse profile info from pkcs7 content: %s", err)
	}
	return serialized.Object(profile.Entitlements), nil
}

func findMissingContainers(projectEnts, profileEnts serialized.Object) ([]string, error) {
	projContainerIDs, err := serialized.Object(projectEnts).StringSlice("com.apple.developer.icloud-container-identifiers")
	if err != nil {
		if serialized.IsKeyNotFoundError(err) {
			return nil, nil // project has no container
		}
		return nil, err
	}

	// project has containers, so the profile should have at least the same

	profContainerIDs, err := serialized.Object(profileEnts).StringSlice("com.apple.developer.icloud-container-identifiers")
	if err != nil {
		if serialized.IsKeyNotFoundError(err) {
			return projContainerIDs, nil
		}
		return nil, err
	}

	// project and profile also has containers, check if profile contains the containers the project need

	var missing []string
	for _, projContainerID := range projContainerIDs {
		var found bool
		for _, profContainerID := range profContainerIDs {
			if projContainerID == profContainerID {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, projContainerID)
		}
	}

	return missing, nil
}

func checkProfileCertificates(profileCertificateIDs map[string]bool, certificateIDs []string) error {
	for _, id := range certificateIDs {
		if !profileCertificateIDs[id] {
			return devportal.NonmatchingProfileError{
				Reason: fmt.Sprintf("certificate with ID (%s) not included in the profile", id),
			}
		}
	}
	return nil
}

func checkProfileDevices(profileDeviceIDs map[string]bool, deviceIDs []string) error {
	for _, id := range deviceIDs {
		if !profileDeviceIDs[id] {
			return devportal.NonmatchingProfileError{
				Reason: fmt.Sprintf("device with ID (%s) not included in the profile", id),
			}
		}
	}

	return nil
}

func isProfileExpired(prof devportal.Profile, minProfileDaysValid int) bool {
	relativeExpiryTime := time.Now()
	if minProfileDaysValid > 0 {
		relativeExpiryTime = relativeExpiryTime.Add(time.Duration(minProfileDaysValid) * 24 * time.Hour)
	}
	return time.Time(prof.Attributes().ExpirationDate).Before(relativeExpiryTime)
}

func checkProfile(client devportal.ProfileClient, prof devportal.Profile, entitlements devportal.Entitlement, deviceIDs, certificateIDs []string, minProfileDaysValid int) error {
	if isProfileExpired(prof, minProfileDaysValid) {
		return devportal.NonmatchingProfileError{
			Reason: fmt.Sprintf("profile expired, or will expire in less then %d day(s)", minProfileDaysValid),
		}
	}

	if err := checkProfileEntitlements(client, prof, entitlements); err != nil {
		return err
	}

	profileCertificateIDs, err := prof.CertificateIDs()
	if err != nil {
		return err
	}
	if err := checkProfileCertificates(profileCertificateIDs, certificateIDs); err != nil {
		return err
	}

	profileDeviceIDs, err := prof.DeviceIDs()
	if err != nil {
		return err
	}
	return checkProfileDevices(profileDeviceIDs, deviceIDs)
}

// CanGenerateProfileWithEntitlements checks all entitlements, wheter they can be generated
func CanGenerateProfileWithEntitlements(entitlementsByBundleID map[string]serialized.Object) (ok bool, badEntitlement string, badBundleID string) {
	for bundleID, entitlements := range entitlementsByBundleID {
		for entitlementKey, value := range entitlements {
			if (devportal.Entitlement{entitlementKey: value}).IsProfileAttached() {
				return false, entitlementKey, bundleID
			}
		}
	}

	return true, "", ""
}

// writeProfile writes the provided profile under the `$HOME/Library/MobileDevice/Provisioning Profiles` directory.
// Xcode uses profiles located in that directory.
// The file extension depends on the profile's platform `IOS` => `.mobileprovision`, `MAC_OS` => `.provisionprofile`
func writeProfile(profile devportal.Profile) error {
	homeDir := os.Getenv("HOME")
	profilesDir := path.Join(homeDir, "Library/MobileDevice/Provisioning Profiles")
	if exists, err := pathutil.IsDirExists(profilesDir); err != nil {
		return fmt.Errorf("failed to check directory (%s) for provisioning profiles: %s", profilesDir, err)
	} else if !exists {
		if err := os.MkdirAll(profilesDir, 0600); err != nil {
			return fmt.Errorf("failed to generate directory (%s) for provisioning profiles: %s", profilesDir, err)
		}
	}

	var ext string
	switch profile.Attributes().Platform {
	case appstoreconnect.IOS:
		ext = ".mobileprovision"
	case appstoreconnect.MacOS:
		ext = ".provisionprofile"
	default:
		return fmt.Errorf("failed to write profile to file, unsupported platform: (%s). Supported platforms: %s, %s", profile.Attributes().Platform, appstoreconnect.IOS, appstoreconnect.MacOS)
	}

	name := path.Join(profilesDir, profile.Attributes().UUID+ext)
	if err := ioutil.WriteFile(name, profile.Attributes().ProfileContent, 0600); err != nil {
		return fmt.Errorf("failed to write profile to file: %s", err)
	}
	return nil
}
