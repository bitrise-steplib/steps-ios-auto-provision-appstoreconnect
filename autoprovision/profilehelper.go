package autoprovision

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// Profile ...
type Profile interface {
	ID() string
	Attributes() appstoreconnect.ProfileAttributes
	CertificateIDs() (map[string]bool, error)
	DeviceIDs() (map[string]bool, error)
	BundleID() (appstoreconnect.BundleID, error)
}

// ProfileClient ...
type ProfileClient interface {
	FindProfile(name string, profileType appstoreconnect.ProfileType) (Profile, error)
	DeleteExpiredProfile(bundleID *appstoreconnect.BundleID, profileName string) error
	DeleteProfile(id string) error
	CreateProfile(name string, profileType appstoreconnect.ProfileType, bundleID appstoreconnect.BundleID, certificateIDs []string, deviceIDs []string) (Profile, error)
	// Bundle ID
	FindBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error)
	CheckBundleIDEntitlements(bundleID appstoreconnect.BundleID, projectEntitlements Entitlement) error
	SyncBundleID(bundleID appstoreconnect.BundleID, entitlements Entitlement) error
	CreateBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error)
}

// APIProfile ...
type APIProfile struct {
	profile *appstoreconnect.Profile
	client  *appstoreconnect.Client
}

// NewAPIProfile ...
func NewAPIProfile(client *appstoreconnect.Client, profile *appstoreconnect.Profile) Profile {
	return &APIProfile{
		profile: profile,
		client:  client,
	}
}

// ID ...
func (p APIProfile) ID() string {
	return p.profile.ID
}

// Attributes ...
func (p APIProfile) Attributes() appstoreconnect.ProfileAttributes {
	return p.profile.Attributes
}

// CertificateIDs ...
func (p APIProfile) CertificateIDs() (map[string]bool, error) {
	var nextPageURL string
	var certificates []appstoreconnect.Certificate
	for {
		response, err := p.client.Provisioning.Certificates(
			p.profile.Relationships.Certificates.Links.Related,
			&appstoreconnect.PagingOptions{
				Limit: 20,
				Next:  nextPageURL,
			},
		)
		if err != nil {
			return nil, wrapInProfileError(err)
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

	return ids, nil
}

// DeviceIDs ...
func (p APIProfile) DeviceIDs() (map[string]bool, error) {
	var nextPageURL string
	ids := map[string]bool{}
	for {
		response, err := p.client.Provisioning.Devices(
			p.profile.Relationships.Devices.Links.Related,
			&appstoreconnect.PagingOptions{
				Limit: 20,
				Next:  nextPageURL,
			},
		)
		if err != nil {
			return nil, wrapInProfileError(err)
		}

		for _, dev := range response.Data {
			ids[dev.ID] = true
		}

		nextPageURL = response.Links.Next
		if nextPageURL == "" {
			break
		}
	}

	return ids, nil
}

// BundleID ...
func (p APIProfile) BundleID() (appstoreconnect.BundleID, error) {
	bundleIDresp, err := p.client.Provisioning.BundleID(p.profile.Relationships.BundleID.Links.Related)
	if err != nil {
		return appstoreconnect.BundleID{}, err
	}

	return bundleIDresp.Data, nil
}

// APIProfileClient ...
type APIProfileClient struct {
	client *appstoreconnect.Client
}

// NewAPIProfileClient ...
func NewAPIProfileClient(client *appstoreconnect.Client) ProfileClient {
	return &APIProfileClient{client: client}
}

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

	prefix := ""
	if strings.HasSuffix(bundleID, ".*") {
		// `*` char is not allowed in Profile name.
		bundleID = strings.TrimSuffix(bundleID, ".*")
		prefix = "Wildcard "
	}

	return fmt.Sprintf("%sBitrise %s %s - (%s)", prefix, platform, distribution, bundleID), nil
}

// FindProfile ...
func (c *APIProfileClient) FindProfile(name string, profileType appstoreconnect.ProfileType) (Profile, error) {
	opt := &appstoreconnect.ListProfilesOptions{
		PagingOptions: appstoreconnect.PagingOptions{
			Limit: 1,
		},
		FilterProfileType: profileType,
		FilterName:        name,
	}

	r, err := c.client.Provisioning.ListProfiles(opt)
	if err != nil {
		return nil, err
	}
	if len(r.Data) == 0 {
		return nil, nil
	}

	return NewAPIProfile(c.client, &r.Data[0]), nil
}

// DeleteExpiredProfile ...
func (c *APIProfileClient) DeleteExpiredProfile(bundleID *appstoreconnect.BundleID, profileName string) error {
	var nextPageURL string
	var profile *appstoreconnect.Profile

	for {
		response, err := c.client.Provisioning.Profiles(bundleID.Relationships.Profiles.Links.Related, &appstoreconnect.PagingOptions{
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

	return c.DeleteProfile(profile.ID)
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

// DeleteProfile ...
func (c *APIProfileClient) DeleteProfile(id string) error {
	if err := c.client.Provisioning.DeleteProfile(id); err != nil {
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
func (c *APIProfileClient) CreateProfile(name string, profileType appstoreconnect.ProfileType, bundleID appstoreconnect.BundleID, certificateIDs []string, deviceIDs []string) (Profile, error) {
	// Create new Bitrise profile on App Store Connect
	r, err := c.client.Provisioning.CreateProfile(
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

	return NewAPIProfile(c.client, &r.Data), nil
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
