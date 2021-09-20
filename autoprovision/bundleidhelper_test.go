package autoprovision

import (
	"testing"

	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

func Test_createWildcardBundleID(t *testing.T) {
	tests := []struct {
		name     string
		bundleID string
		want     string
		wantErr  bool
	}{
		{
			name:     "Invalid bundle id: empty",
			bundleID: "",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "Invalid bundle id: does not contain *",
			bundleID: "my_app",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "2 component bundle id",
			bundleID: "com.my_app",
			want:     "com.*",
			wantErr:  false,
		},
		{
			name:     "multi component bundle id",
			bundleID: "com.bitrise.my_app.uitest",
			want:     "com.bitrise.my_app.*",
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createWildcardBundleID(tt.bundleID)
			if (err != nil) != tt.wantErr {
				t.Errorf("createWildcardBundleID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("createWildcardBundleID() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_checkBundleIDEntitlements(t *testing.T) {
	tests := []struct {
		name                 string
		bundleIDEntitlements []appstoreconnect.BundleIDCapability
		projectEntitlements  Entitlement
		wantErr              bool
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
			wantErr: false,
		},
		{
			name:                 "Needed to register entitlements",
			bundleIDEntitlements: []appstoreconnect.BundleIDCapability{},
			projectEntitlements: Entitlement(map[string]interface{}{
				"com.apple.developer.applesignin": "",
			}),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkBundleIDEntitlements(tt.bundleIDEntitlements, tt.projectEntitlements)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkBundleIDEntitlements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if mErr, ok := err.(NonmatchingProfileError); !ok {
					t.Errorf("checkBundleIDEntitlements() error = %v, it is not expected type", mErr)
				}
			}
		})
	}
}
