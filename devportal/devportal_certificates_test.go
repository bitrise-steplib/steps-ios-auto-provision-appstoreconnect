package devportal

import (
	_ "embed"
	"reflect"
	"testing"

	"github.com/bitrise-io/go-xcode/certificateutil"
)

func Test_getAllCertificates(t *testing.T) {
	tests := []struct {
		name    string
		want    []certificateutil.CertificateInfoModel
		wantErr bool
	}{
		{
			name: "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAllCertificates()
			if (err != nil) != tt.wantErr {
				t.Errorf("getAllCertificates() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getAllCertificates() = %v, want %v", got, tt.want)
			}
		})
	}
}
