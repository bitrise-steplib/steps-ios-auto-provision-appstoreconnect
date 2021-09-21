package autoprovision

import (
	"testing"
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
