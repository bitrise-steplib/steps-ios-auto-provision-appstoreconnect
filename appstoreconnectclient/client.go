package appstoreconnectclient

import (
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/devportal"
)

// NewAPIDevportalClient ...
func NewAPIDevportalClient(client *appstoreconnect.Client) devportal.DevportalClient {
	return devportal.DevportalClient{
		CertificateSource: NewAPICertificateSource(client),
		DeviceClient:      NewAPIDeviceClient(client),
		ProfileClient:     NewAPIProfileClient(client),
	}
}
