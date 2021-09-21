package autoprovision

import (
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/devportal"
)

// CanGenerateProfileWithEntitlements checks all entitlements, wheter they can be generated
func CanGenerateProfileWithEntitlements(entitlementsByBundleID map[string]serialized.Object) (ok bool, badEntitlement string, badBundleID string) {
	for bundleID, entitlements := range entitlementsByBundleID {
		for entitlementKey, value := range entitlements {
			if (devportal.Entitlement{entitlementKey: value}).IsProfileAttached() {
				return false, entitlementKey, bundleID
			}
		}
	}

	return true, "", ""
}
