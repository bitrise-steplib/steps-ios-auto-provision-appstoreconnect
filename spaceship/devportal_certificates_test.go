package spaceship

import (
	_ "embed"
	"testing"
)

// func Test_getAllCertificates(t *testing.T) {
// 	tests := []struct {
// 		name    string
// 		want    []certificateutil.CertificateInfoModel
// 		wantErr bool
// 	}{
// 		{
// 			name: "",
// 			want: nil,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			got := NewSpaceshipCertificateSource()
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("getAllCertificates() error = %v, wantErr %v", err, tt.wantErr)
// 				return
// 			}
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("getAllCertificates() = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }

func Test_createProfile(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := createProfile(); (err != nil) != tt.wantErr {
				t.Errorf("createProfile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
