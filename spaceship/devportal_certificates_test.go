package spaceship

import (
	_ "embed"
	"testing"
)

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
