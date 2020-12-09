package autoprovision_test

import (
	"testing"

	"github.com/bitrise-io/xcode-project/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
	"github.com/stretchr/testify/require"
)

func TestICloudContainers(t *testing.T) {
	tests := []struct {
		name                string
		projectEntitlements autoprovision.Entitlement
		want                []string
		errHandler          func(require.TestingT, error, ...interface{})
	}{
		{
			name:                "no containers",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{}),
			want:                nil,
			errHandler:          require.NoError,
		},
		{
			name: "no containers - CloudDocuments",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{
					"CloudDocuments",
				},
			}),
			want:       nil,
			errHandler: require.NoError,
		},
		{
			name: "no containers - CloudKit",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{
					"CloudKit",
				},
			}),
			want:       nil,
			errHandler: require.NoError,
		},
		{
			name: "no containers - CloudKit and CloudDocuments",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{
					"CloudKit",
					"CloudDocuments",
				},
			}),
			want:       nil,
			errHandler: require.NoError,
		},
		{
			name: "has containers - CloudDocuments",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{
					"CloudDocuments",
				},
				"com.apple.developer.icloud-container-identifiers": []interface{}{
					"iCloud.test.container.id",
					"iCloud.test.container.id2"},
			}),
			want:       []string{"iCloud.test.container.id", "iCloud.test.container.id2"},
			errHandler: require.NoError,
		},
		{
			name: "has containers - CloudKit",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{
					"CloudKit",
				},
				"com.apple.developer.icloud-container-identifiers": []interface{}{
					"iCloud.test.container.id",
					"iCloud.test.container.id2"},
			}),
			want:       []string{"iCloud.test.container.id", "iCloud.test.container.id2"},
			errHandler: require.NoError,
		},
		{
			name: "has containers - CloudKit and CloudDocuments",
			projectEntitlements: autoprovision.Entitlement(map[string]interface{}{
				"com.apple.developer.icloud-services": []interface{}{
					"CloudKit",
					"CloudDocuments",
				},
				"com.apple.developer.icloud-container-identifiers": []interface{}{
					"iCloud.test.container.id",
					"iCloud.test.container.id2"},
			}),
			want:       []string{"iCloud.test.container.id", "iCloud.test.container.id2"},
			errHandler: require.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.projectEntitlements.ICloudContainers()
			require.Equal(t, got, tt.want)
			tt.errHandler(t, err)
		})
	}
}

func TestCanGenerateProfileWithEntitlements(t *testing.T) {
	tests := []struct {
		name                   string
		entitlementsByBundleID map[string]serialized.Object
		want                   bool
		wantEntitlement        string
		wantBundleID           string
	}{
		{
			name: "no entitlements",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{},
			},
			want:            true,
			wantEntitlement: "",
			wantBundleID:    "",
		},
		{
			name: "contains unsupported entitlement",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{
					"com.entitlement-ignored":            true,
					"com.apple.developer.contacts.notes": true,
				},
			},
			want:            false,
			wantEntitlement: "com.apple.developer.contacts.notes",
			wantBundleID:    "com.bundleid",
		},
		{
			name: "contains unsupported entitlement, multiple bundle IDs",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{
					"aps-environment": true,
				},
				"com.bundleid2": map[string]interface{}{
					"com.entitlement-ignored":            true,
					"com.apple.developer.contacts.notes": true,
				},
			},
			want:            false,
			wantEntitlement: "com.apple.developer.contacts.notes",
			wantBundleID:    "com.bundleid2",
		},
		{
			name: "all entitlements supported",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{
					"aps-environment": true,
				},
			},
			want:            true,
			wantEntitlement: "",
			wantBundleID:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotEntilement, gotBundleID := autoprovision.CanGenerateProfileWithEntitlements(tt.entitlementsByBundleID)
			if got != tt.want {
				t.Errorf("CanGenerateProfileWithEntitlements() got = %v, want %v", got, tt.want)
			}
			if gotEntilement != tt.wantEntitlement {
				t.Errorf("CanGenerateProfileWithEntitlements() got1 = %v, want %v", gotEntilement, tt.wantEntitlement)
			}
			if gotBundleID != tt.wantBundleID {
				t.Errorf("CanGenerateProfileWithEntitlements() got2 = %v, want %v", gotBundleID, tt.wantBundleID)
			}
		})
	}
}
