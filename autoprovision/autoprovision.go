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

type DeviceLister interface {
	ListDevices(udid string, platform appstoreconnect.DevicePlatform) ([]appstoreconnect.Device, error)
}

type DevportalClient struct {
	CertificateSource CertificateSource
	DeviceLister      DeviceLister
	ProfileClient     ProfileClient
}

func NewAPIDevportalClient(client *appstoreconnect.Client) DevportalClient {
	return DevportalClient{
		CertificateSource: NewAPICertificateSource(client),
		DeviceLister:      NewAPIDeviceLister(client),
		ProfileClient:     NewAPIProfileClient(client),
	}
}
