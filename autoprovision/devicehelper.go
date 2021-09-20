package autoprovision

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

func registerMissingTestDevices(client DeviceClient, testDevices []devportalservice.TestDevice, devPortalDevices []appstoreconnect.Device) ([]appstoreconnect.Device, error) {
	if client == nil {
		return []appstoreconnect.Device{}, fmt.Errorf("the App Store Connect API client not provided")
	}

	var newDevPortalDevices []appstoreconnect.Device

	for _, testDevice := range testDevices {
		log.Printf("checking if the device (%s) is registered", testDevice.DeviceID)

		devPortalDevice := findDevPortalDevice(testDevice, devPortalDevices)
		if devPortalDevice != nil {
			log.Printf("device already registered")
			continue
		}

		log.Printf("registering device")
		newDevPortalDevice, err := client.RegisterDevice(testDevice)
		if err != nil {
			var registrationError appstoreconnect.DeviceRegistrationError
			if errors.As(err, &registrationError) {
				log.Warnf("Failed to register device (can be caused by invalid UDID or trying to register a Mac device): %s", registrationError.Reason)
				return nil, nil
			}

			return nil, err
		}

		if newDevPortalDevice != nil {
			newDevPortalDevices = append(newDevPortalDevices, *newDevPortalDevice)
		}
	}

	return newDevPortalDevices, nil
}

func findDevPortalDevice(testDevice devportalservice.TestDevice, devPortalDevices []appstoreconnect.Device) *appstoreconnect.Device {
	for _, devPortalDevice := range devPortalDevices {
		if devportalservice.IsEqualUDID(devPortalDevice.Attributes.UDID, testDevice.DeviceID) {
			return &devPortalDevice
		}
	}
	return nil
}

func filterDevPortalDevices(devPortalDevices []appstoreconnect.Device, platform Platform) []appstoreconnect.Device {
	var filteredDevices []appstoreconnect.Device

	for _, devPortalDevice := range devPortalDevices {
		deviceClass := devPortalDevice.Attributes.DeviceClass

		switch platform {
		case IOS:
			isIosOrWatchosDevice := deviceClass == appstoreconnect.AppleWatch ||
				deviceClass == appstoreconnect.Ipad ||
				deviceClass == appstoreconnect.Iphone ||
				deviceClass == appstoreconnect.Ipod

			if isIosOrWatchosDevice {
				filteredDevices = append(filteredDevices, devPortalDevice)
			}
		case TVOS:
			if deviceClass == appstoreconnect.AppleTV {
				filteredDevices = append(filteredDevices, devPortalDevice)
			}
		}
	}

	return filteredDevices
}

// DistributionTypeRequiresDeviceList ...
func DistributionTypeRequiresDeviceList(distrTypes []DistributionType) bool {
	for _, distrType := range distrTypes {
		if distrType == Development || distrType == AdHoc {
			return true
		}
	}
	return false
}
