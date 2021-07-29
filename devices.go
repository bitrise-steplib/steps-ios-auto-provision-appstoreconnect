package main

import (
	"fmt"
	"net/http"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

func needToRegisterDevices(distrTypes []autoprovision.DistributionType) bool {
	for _, distrType := range distrTypes {
		if distrType == autoprovision.Development || distrType == autoprovision.AdHoc {
			return true
		}
	}
	return false
}

func findDevPortalDevice(testDevice devportalservice.TestDevice, devPortalDevices []appstoreconnect.Device) *appstoreconnect.Device {
	for _, devPortalDevice := range devPortalDevices {
		if devportalservice.IsEqualUDID(devPortalDevice.Attributes.UDID, testDevice.DeviceID) {
			return &devPortalDevice
		}
	}
	return nil
}

func registerTestDeviceOnDevPortal(client *appstoreconnect.Client, testDevice devportalservice.TestDevice) (*appstoreconnect.Device, error) {
	// The API seems to recognize existing devices even with different casing and '-' separator removed.
	// The Developer Portal UI does not let adding devices with unexpected casing or separators removed.
	// Did not fully validate the ability to add devices with changed casing (or '-' removed) via the API, so passing the UDID through unchanged.
	log.Printf("registering device")
	req := appstoreconnect.DeviceCreateRequest{
		Data: appstoreconnect.DeviceCreateRequestData{
			Attributes: appstoreconnect.DeviceCreateRequestDataAttributes{
				Name:     "Bitrise test device",
				Platform: appstoreconnect.IOS,
				UDID:     testDevice.DeviceID,
			},
			Type: "devices",
		},
	}

	registeredDevice, err := client.Provisioning.RegisterNewDevice(req)
	if err != nil {
		rerr, ok := err.(*appstoreconnect.ErrorResponse)
		if ok && rerr.Response != nil && rerr.Response.StatusCode == http.StatusConflict {
			log.Warnf("Failed to register device (can be caused by invalid UDID or trying to register a Mac device): %s", err)
			return nil, nil
		}

		return nil, err
	}

	return &registeredDevice.Data, nil
}

func registerMissingTestDevices(client *appstoreconnect.Client, testDevices []devportalservice.TestDevice, devPortalDevices []appstoreconnect.Device) ([]appstoreconnect.Device, error) {
	if client == nil {
		return []appstoreconnect.Device{}, fmt.Errorf("the App Store Connect API client not provided")
	}

	var newDevPortalDevices []appstoreconnect.Device

	for _, testDevice := range testDevices {
		devPortalDevice := findDevPortalDevice(testDevice, devPortalDevices)
		if devPortalDevice != nil {
			log.Printf("device already registered")
			continue
		}

		newDevPortalDevice, err := registerTestDeviceOnDevPortal(client, testDevice)
		if err != nil {
			return nil, err
		}

		if newDevPortalDevice != nil {
			newDevPortalDevices = append(newDevPortalDevices, *newDevPortalDevice)
		}
	}

	return newDevPortalDevices, nil
}

func filterDevPortalDevices(devPortalDevices []appstoreconnect.Device, platform autoprovision.Platform) []appstoreconnect.Device {
	var filteredDevices []appstoreconnect.Device

	for _, devPortalDevice := range devPortalDevices {
		deviceClass := devPortalDevice.Attributes.DeviceClass

		switch platform {
		case autoprovision.IOS:
			isIosOrWatchosDevice := deviceClass == appstoreconnect.AppleWatch ||
				deviceClass == appstoreconnect.Ipad ||
				deviceClass == appstoreconnect.Iphone ||
				deviceClass == appstoreconnect.Ipod

			if isIosOrWatchosDevice {
				filteredDevices = append(filteredDevices, devPortalDevice)
			}
		case autoprovision.TVOS:
			if deviceClass == appstoreconnect.AppleTV {
				filteredDevices = append(filteredDevices, devPortalDevice)
			}
		}
	}

	return filteredDevices
}

func listRelevantDevPortalDevices(client *appstoreconnect.Client, testDevices []devportalservice.TestDevice, platform autoprovision.Platform) ([]appstoreconnect.Device, error) {
	fmt.Println()
	log.Infof("Fetching Apple Developer Portal devices")

	devPortalDevices, err := autoprovision.ListDevices(client, "", appstoreconnect.IOSDevice)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch devices: %s", err)
	}

	log.Printf("%d devices are registered on the Apple Developer Portal", len(devPortalDevices))
	for _, devPortalDevice := range devPortalDevices {
		log.Debugf("- %s, %s, UDID (%s), ID (%s)", devPortalDevice.Attributes.Name, devPortalDevice.Attributes.DeviceClass, devPortalDevice.Attributes.UDID, devPortalDevice.ID)
	}

	if len(testDevices) != 0 {
		log.Infof("Checking if %d Bitrise test device(s) are registered on Apple Developer Portal", len(testDevices))
		for _, testDevice := range testDevices {
			log.Debugf("- %s, %s, UDID (%s), added at %s", testDevice.Title, testDevice.DeviceType, testDevice.DeviceID, testDevice.UpdatedAt)
		}

		devPortalDevice, err := registerMissingTestDevices(client, testDevices, devPortalDevices)
		if err != nil {
			return nil, fmt.Errorf("failed to register Bitrise Test device on Apple Developer Portal: %s", err)
		}
		devPortalDevices = append(devPortalDevices, devPortalDevice...)
	}

	return filterDevPortalDevices(devPortalDevices, platform), nil
}
