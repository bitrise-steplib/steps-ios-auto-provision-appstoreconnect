package devportaldata

import (
	"reflect"
	"testing"
)

func TestDownloader_parseDevPortalData(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    *DevPortalData
		wantErr bool
	}{
		{
			name:    "empty data",
			data:    []byte(""),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty fields",
			data:    []byte(`{"key_id":"","issuer_id":"","private_key":""}`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "valid fields",
			data:    []byte(`{"key_id":"key","issuer_id":"issuer","private_key":"p8"}`),
			want:    &DevPortalData{KeyID: "key", IssuerID: "issuer", PrivateKey: "p8"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Downloader{}
			got, err := c.parseDevPortalData(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Downloader.parseDevPortalData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Downloader.parseDevPortalData() = %v, want %v", got, tt.want)
			}
		})
	}
}
