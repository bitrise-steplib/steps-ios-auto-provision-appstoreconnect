package autoprovision

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// ProfileName generates profile name with layout: Bitrise <platform> <distribution type> - (<bundle id>)
func ProfileName(profileType appstoreconnect.ProfileType, bundleID string) (string, error) {
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

func checkProfileEntitlements(client ProfileClient, prof Profile, projectEntitlements Entitlement) error {
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
		return NonmatchingProfileError{
			Reason: fmt.Sprintf("project uses containers that are missing from the provisioning profile: %v", missingContainers),
		}
	}

	bundleID, err := prof.BundleID()
	if err != nil {
		return err
	}

	return client.CheckBundleIDEntitlements(bundleID, projectEntitlements)
}

func parseRawProfileEntitlements(prof Profile) (serialized.Object, error) {
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
			return NonmatchingProfileError{
				Reason: fmt.Sprintf("certificate with ID (%s) not included in the profile", id),
			}
		}
	}
	return nil
}

func checkProfileDevices(profileDeviceIDs map[string]bool, deviceIDs []string) error {
	for _, id := range deviceIDs {
		if !profileDeviceIDs[id] {
			return NonmatchingProfileError{
				Reason: fmt.Sprintf("device with ID (%s) not included in the profile", id),
			}
		}
	}

	return nil
}

// IsProfileExpired ...
func IsProfileExpired(prof Profile, minProfileDaysValid int) bool {
	relativeExpiryTime := time.Now()
	if minProfileDaysValid > 0 {
		relativeExpiryTime = relativeExpiryTime.Add(time.Duration(minProfileDaysValid) * 24 * time.Hour)
	}
	return time.Time(prof.Attributes().ExpirationDate).Before(relativeExpiryTime)
}

// CheckProfile ...
func CheckProfile(client ProfileClient, prof Profile, entitlements Entitlement, deviceIDs, certificateIDs []string, minProfileDaysValid int) error {
	if IsProfileExpired(prof, minProfileDaysValid) {
		return NonmatchingProfileError{
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

// WriteProfile writes the provided profile under the `$HOME/Library/MobileDevice/Provisioning Profiles` directory.
// Xcode uses profiles located in that directory.
// The file extension depends on the profile's platform `IOS` => `.mobileprovision`, `MAC_OS` => `.provisionprofile`
func WriteProfile(profile Profile) error {
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
