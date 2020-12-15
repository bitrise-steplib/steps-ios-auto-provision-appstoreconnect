package autoprovision

import (
	"testing"

	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

func Test_checkBundleIDEntitlements(t *testing.T) {
	tests := []struct {
		name                 string
		bundleIDEntitlements []appstoreconnect.BundleIDCapability
		projectEntitlements  Entitlement
		wantOk               bool
		wantErr              bool
		wantReason           string
	}{
		{
			name:                 "Check known entitlements, which does not need to be registered on the Developer Portal",
			bundleIDEntitlements: []appstoreconnect.BundleIDCapability{},
			projectEntitlements: Entitlement(map[string]interface{}{
				"keychain-access-groups":                             "",
				"com.apple.developer.ubiquity-kvstore-identifier":    "",
				"com.apple.developer.icloud-container-identifiers":   "",
				"com.apple.developer.ubiquity-container-identifiers": "",
			}),
			wantOk:     true,
			wantErr:    false,
			wantReason: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOk, gotReason, err := checkBundleIDEntitlements(tt.bundleIDEntitlements, tt.projectEntitlements)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkBundleIDEntitlements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotOk != tt.wantOk {
				t.Errorf("checkBundleIDEntitlements() ok = %v, want %v", gotOk, tt.wantOk)
			}
			if gotReason != tt.wantReason {
				t.Errorf("checkBundleIDEntitlements() reason = %v, want %v", gotReason, tt.wantReason)
			}
		})
	}
}
