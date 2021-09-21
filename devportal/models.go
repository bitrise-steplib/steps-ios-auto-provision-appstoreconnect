package devportal

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/bitrise-io/go-xcode/devportalservice"

	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// DevportalClient ...
type DevportalClient struct {
	CertificateSource CertificateSource
	DeviceClient      DeviceClient
	ProfileClient     ProfileClient
}

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

// ProfileClient ...
type ProfileClient interface {
	FindProfile(name string, profileType appstoreconnect.ProfileType) (Profile, error)
	DeleteProfile(id string) error
	CreateProfile(name string, profileType appstoreconnect.ProfileType, bundleID appstoreconnect.BundleID, certificateIDs []string, deviceIDs []string) (Profile, error)
	// Bundle ID
	FindBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error)
	CheckBundleIDEntitlements(bundleID appstoreconnect.BundleID, projectEntitlements Entitlement) error
	SyncBundleID(bundleID appstoreconnect.BundleID, entitlements Entitlement) error
	CreateBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error)
}

// DeviceClient ...
type DeviceClient interface {
	ListDevices(udid string, platform appstoreconnect.DevicePlatform) ([]appstoreconnect.Device, error)
	RegisterDevice(testDevice devportalservice.TestDevice) (*appstoreconnect.Device, error)
}

// Profile ...
type Profile interface {
	ID() string
	Attributes() appstoreconnect.ProfileAttributes
	CertificateIDs() (map[string]bool, error)
	DeviceIDs() (map[string]bool, error)
	BundleID() (appstoreconnect.BundleID, error)
}

// AppIDName ...
func AppIDName(bundleID string) string {
	prefix := ""
	if strings.HasSuffix(bundleID, ".*") {
		prefix = "Wildcard "
	}
	r := strings.NewReplacer(".", " ", "_", " ", "-", " ", "*", " ")
	return prefix + "Bitrise " + r.Replace(bundleID)
}

// NonmatchingProfileError is returned when a profile/bundle ID does not match project requirements
// It is not a fatal error, as the profile can be regenerated
type NonmatchingProfileError struct {
	Reason string
}

func (e NonmatchingProfileError) Error() string {
	return fmt.Sprintf("provisioning profile does not match requirements: %s", e.Reason)
}
