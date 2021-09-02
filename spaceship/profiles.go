package spaceship

import (
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

type SpaceshipProfileClient struct {
	client *Client
}

func NewSpaceshipProfileClient(client *Client) SpaceshipProfileClient {
	return SpaceshipProfileClient{client: client}
}

func (c *SpaceshipProfileClient) FindProfile(name string, profileType appstoreconnect.ProfileType) (*appstoreconnect.Profile, error) {
	panic("implement")
	// cmd, err := c.client.createRequestCommand("find_profile",
	// 	"--profile_name", name,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	// output, err := runSpaceshipCommand(cmd)
	// fmt.Println(output)
	// return nil, err
}

func (c *SpaceshipProfileClient) DeleteExpiredProfile(bundleID *appstoreconnect.BundleID, profileName string) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) CheckProfile(prof appstoreconnect.Profile, entitlements autoprovision.Entitlement, deviceIDs, certificateIDs []string, minProfileDaysValid int) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) DeleteProfile(id string) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) CreateProfile(name string, profileType appstoreconnect.ProfileType, bundleID appstoreconnect.BundleID, certificateIDs []string, deviceIDs []string) (*appstoreconnect.Profile, error) {
	panic("implement")
}

func (c *SpaceshipProfileClient) FindBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error) {
	panic("implement")
}

func (c *SpaceshipProfileClient) CheckBundleIDEntitlements(bundleID appstoreconnect.BundleID, projectEntitlements autoprovision.Entitlement) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) SyncBundleID(bundleIDID string, entitlements autoprovision.Entitlement) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) CreateBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error) {
	panic("implement")
}
