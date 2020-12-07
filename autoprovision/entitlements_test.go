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

func TestContainsUnsupportedEntitlement(t *testing.T) {
	tests := []struct {
		name                    string
		entitlementsByBundleID  map[string]serialized.Object
		unsupportedEntitlements []string
		wantErr                 bool
	}{
		{
			name: "no entitlements",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{},
			},
			unsupportedEntitlements: []string{"com.entitlements-unsupported"},
		},
		{
			name: "contains unsupported entitlement",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{
					"com.entitlement-supported":   true,
					"com.entitlement-unsupported": true,
				},
			},
			unsupportedEntitlements: []string{"com.entitlement-unsupported"},
			wantErr:                 true,
		},
		{
			name: "contains unsupported entitlement, multiple bundle IDs",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleID1": map[string]interface{}{
					"com.entitlement-supported": true,
				},
				"com.bundleid": map[string]interface{}{
					"com.entitlement-supported":   true,
					"com.entitlement-unsupported": true,
				},
			},
			unsupportedEntitlements: []string{"com.entitlement-unsupported"},
			wantErr:                 true,
		},
		{
			name: "all entitlements supported",
			entitlementsByBundleID: map[string]serialized.Object{
				"com.bundleid": map[string]interface{}{
					"com.entitlement-supported": true,
				},
			},
			unsupportedEntitlements: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := autoprovision.ContainsUnsupportedEntitlement(tt.entitlementsByBundleID, tt.unsupportedEntitlements); (err != nil) != tt.wantErr {
				t.Errorf("ContainsUnsupportedEntitlement() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
