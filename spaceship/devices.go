package spaceship

import "github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"

type SpaceshipDeviceLister struct{}

func (*SpaceshipDeviceLister) ListDevices(udid string, platform appstoreconnect.DevicePlatform) ([]appstoreconnect.Device, error) {
	return nil, nil
}
