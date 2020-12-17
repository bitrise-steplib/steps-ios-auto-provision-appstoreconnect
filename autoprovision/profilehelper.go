package autoprovision

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/xcode-project/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// NonmatchingProfileError is returned when a profile/bundle ID does not match project requirements
// It is not a fatal error, as the profile can be regenerated
type NonmatchingProfileError struct {
	Reason string
}

func (e NonmatchingProfileError) Error() string {
	return fmt.Sprintf("provisioning profile does not match requirements: %s", e.Reason)
}

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

	return fmt.Sprintf("Bitrise %s %s - (%s)", platform, distribution, bundleID), nil
}

// FindProfile ...
func FindProfile(client *appstoreconnect.Client, name string, profileType appstoreconnect.ProfileType, bundleIDIdentifier string) (*appstoreconnect.Profile, error) {
	opt := &appstoreconnect.ListProfilesOptions{
		PagingOptions: appstoreconnect.PagingOptions{
			Limit: 1,
		},
		FilterProfileType: profileType,
		FilterName:        name,
	}

	r, err := client.Provisioning.ListProfiles(opt)
	if err != nil {
		return nil, err
	}
	if len(r.Data) == 0 {
		return nil, nil
	}

	return &r.Data[0], nil
}

func wrapInProfileError(err error) error {
	if respErr, ok := err.(appstoreconnect.ErrorResponse); ok {
		if respErr.Response != nil && respErr.Response.StatusCode == http.StatusNotFound {
			return NonmatchingProfileError{
				Reason: fmt.Sprintf("profile was concurrently removed from Developer Portal: %v", err),
			}
		}
	}

	return err
}

func checkProfileEntitlements(client *appstoreconnect.Client, prof appstoreconnect.Profile, projectEntitlements Entitlement) error {
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

	bundleIDresp, err := client.Provisioning.BundleID(prof.Relationships.BundleID.Links.Related)
	if err != nil {
		return err
	}

	return CheckBundleIDEntitlements(client, bundleIDresp.Data, projectEntitlements)
}

func parseRawProfileEntitlements(prof appstoreconnect.Profile) (serialized.Object, error) {
	pkcs, err := profileutil.ProvisioningProfileFromContent(prof.Attributes.ProfileContent)
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

func checkProfileCertificates(client *appstoreconnect.Client, prof appstoreconnect.Profile, certificateIDs []string) error {
	var nextPageURL string
	var certificates []appstoreconnect.Certificate
	for {
		response, err := client.Provisioning.Certificates(
			prof.Relationships.Certificates.Links.Related,
			&appstoreconnect.PagingOptions{
				Limit: 20,
				Next:  nextPageURL,
			},
		)
		if err != nil {
			return wrapInProfileError(err)
		}

		certificates = append(certificates, response.Data...)

		nextPageURL = response.Links.Next
		if nextPageURL == "" {
			break
		}
	}

	ids := map[string]bool{}
	for _, cert := range certificates {
		ids[cert.ID] = true
	}
	for _, id := range certificateIDs {
		if !ids[id] {
			return NonmatchingProfileError{
				Reason: fmt.Sprintf("certificate with ID (%s) not included in the profile", id),
			}
		}
	}
	return nil
}

func checkProfileDevices(client *appstoreconnect.Client, prof appstoreconnect.Profile, deviceIDs []string) error {
	var nextPageURL string
	ids := map[string]bool{}
	for {
		response, err := client.Provisioning.Devices(
			prof.Relationships.Devices.Links.Related,
			&appstoreconnect.PagingOptions{
				Limit: 20,
				Next:  nextPageURL,
			},
		)
		if err != nil {
			return wrapInProfileError(err)
		}

		for _, dev := range response.Data {
			ids[dev.ID] = true
		}

		nextPageURL = response.Links.Next
		if nextPageURL == "" {
			break
		}
	}

	for _, id := range deviceIDs {
		if !ids[id] {
			return NonmatchingProfileError{
				Reason: fmt.Sprintf("device with ID (%s) not included in the profile", id),
			}
		}
	}

	return nil
}

func isProfileExpired(prof appstoreconnect.Profile, minProfileDaysValid int) bool {
	relativeExpiryTime := time.Now()
	if minProfileDaysValid > 0 {
		relativeExpiryTime = relativeExpiryTime.Add(time.Duration(minProfileDaysValid) * 24 * time.Hour)
	}
	return time.Time(prof.Attributes.ExpirationDate).Before(relativeExpiryTime)
}

// CheckProfile ...
func CheckProfile(client *appstoreconnect.Client, prof appstoreconnect.Profile, entitlements Entitlement, deviceIDs, certificateIDs []string, minProfileDaysValid int) error {
	if isProfileExpired(prof, minProfileDaysValid) {
		return NonmatchingProfileError{
			Reason: fmt.Sprintf("profile expired, or will expire in less then %d day(s)", minProfileDaysValid),
		}
	}

	if err := checkProfileEntitlements(client, prof, entitlements); err != nil {
		return err
	}

	if err := checkProfileCertificates(client, prof, certificateIDs); err != nil {
		return err
	}

	return checkProfileDevices(client, prof, deviceIDs)
}

// DeleteProfile ...
func DeleteProfile(client *appstoreconnect.Client, id string) error {
	if err := client.Provisioning.DeleteProfile(id); err != nil {
		if respErr, ok := err.(appstoreconnect.ErrorResponse); ok {
			if respErr.Response != nil && respErr.Response.StatusCode == http.StatusNotFound {
				return nil
			}
		}

		return err
	}

	return nil
}

// CreateProfile ...
func CreateProfile(client *appstoreconnect.Client, name string, profileType appstoreconnect.ProfileType, bundleID appstoreconnect.BundleID, certificateIDs []string, deviceIDs []string) (*appstoreconnect.Profile, error) {
	// Create new Bitrise profile on App Store Connect
	r, err := client.Provisioning.CreateProfile(
		appstoreconnect.NewProfileCreateRequest(
			profileType,
			name,
			bundleID.ID,
			certificateIDs,
			deviceIDs,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s provisioning profile for %s bundle ID: %s", profileType.ReadableString(), bundleID.Attributes.Identifier, err)
	}
	return &r.Data, nil
}

// WriteProfile writes the provided profile under the `$HOME/Library/MobileDevice/Provisioning Profiles` directory.
// Xcode uses profiles located in that directory.
// The file extension depends on the profile's platform `IOS` => `.mobileprovision`, `MAC_OS` => `.provisionprofile`
func WriteProfile(profile appstoreconnect.Profile) error {
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
	switch profile.Attributes.Platform {
	case appstoreconnect.IOS:
		ext = ".mobileprovision"
	case appstoreconnect.MacOS:
		ext = ".provisionprofile"
	default:
		return fmt.Errorf("failed to write profile to file, unsupported platform: (%s). Supported platforms: %s, %s", profile.Attributes.Platform, appstoreconnect.IOS, appstoreconnect.MacOS)
	}

	name := path.Join(profilesDir, profile.Attributes.UUID+ext)
	if err := ioutil.WriteFile(name, profile.Attributes.ProfileContent, 0600); err != nil {
		return fmt.Errorf("failed to write profile to file: %s", err)
	}
	return nil
}
