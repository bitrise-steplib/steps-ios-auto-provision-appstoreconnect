package autocodesign

import (
	"fmt"

	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// MissingCertificateError ...
type MissingCertificateError struct {
	Type   appstoreconnect.CertificateType
	TeamID string
}

func (e MissingCertificateError) Error() string {
	return fmt.Sprintf("no valid %s type certificates uploaded with Team ID (%s)\n ", e.Type, e.TeamID)
}
