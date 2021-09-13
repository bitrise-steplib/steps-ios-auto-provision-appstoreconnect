package autoprovision

import (
	"math/big"

	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
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

// DeviceLister ...
type DeviceLister interface {
	ListDevices(udid string, platform appstoreconnect.DevicePlatform) ([]appstoreconnect.Device, error)
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
