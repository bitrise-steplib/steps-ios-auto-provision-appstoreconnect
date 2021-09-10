package spaceship

import "github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"

// DeviceLister ...
type DeviceLister struct{}

// ListDevices ...
func (*DeviceLister) ListDevices(udid string, platform appstoreconnect.DevicePlatform) ([]appstoreconnect.Device, error) {
	return nil, nil
}
