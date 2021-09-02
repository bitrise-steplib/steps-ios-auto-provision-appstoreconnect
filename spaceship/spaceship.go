package spaceship

import (
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

type Client struct {
	workDir string
}

func NewClient() (Client, error) {
	dir, err := getSpaceshipDirectory()
	if err != nil {
		return Client{}, err
	}

	return Client{
		workDir: dir,
	}, nil
}

func NewSpaceshipDevportalClient(client Client) autoprovision.DevportalClient {
	return autoprovision.DevportalClient{
		CertificateSource: NewSpaceshipCertificateSource(client),
		DeviceLister:      &SpaceshipDeviceLister{},
	}
}
