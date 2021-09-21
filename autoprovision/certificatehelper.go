package autoprovision

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/devportal"
)

// DistributionType ...
type DistributionType string

// DistributionTypes ...
var (
	Development DistributionType = "development"
	AppStore    DistributionType = "app-store"
	AdHoc       DistributionType = "ad-hoc"
	Enterprise  DistributionType = "enterprise"
)

// CertificateTypeByDistribution ...
var CertificateTypeByDistribution = map[DistributionType]appstoreconnect.CertificateType{
	Development: appstoreconnect.IOSDevelopment,
	AppStore:    appstoreconnect.IOSDistribution,
	AdHoc:       appstoreconnect.IOSDistribution,
	Enterprise:  appstoreconnect.IOSDistribution,
}

// CertsToString ...
func CertsToString(certs []certificateutil.CertificateInfoModel) (s string) {
	for i, cert := range certs {
		s += "- "
		s += cert.String()
		if i < len(certs)-1 {
			s += "\n"
		}
	}
	return
}

// MissingCertificateError ...
type MissingCertificateError struct {
	Type   appstoreconnect.CertificateType
	TeamID string
}

func (e MissingCertificateError) Error() string {
	return fmt.Sprintf("no valid %s type certificates uploaded with Team ID (%s)\n ", e.Type, e.TeamID)
}

// GetValidCertificates ...
func GetValidCertificates(localCertificates []certificateutil.CertificateInfoModel, client devportal.CertificateSource, requiredCertificateTypes map[appstoreconnect.CertificateType]bool, teamID string, isDebugLog bool) (map[appstoreconnect.CertificateType][]devportal.APICertificate, error) {
	typeToLocalCerts, err := GetValidLocalCertificates(localCertificates, teamID)
	if err != nil {
		return nil, err
	}

	log.Debugf("Certificates required for Development: %t; Distribution: %t", requiredCertificateTypes[appstoreconnect.IOSDevelopment], requiredCertificateTypes[appstoreconnect.IOSDistribution])

	for certificateType, required := range requiredCertificateTypes {
		if required && len(typeToLocalCerts[certificateType]) == 0 {
			return map[appstoreconnect.CertificateType][]devportal.APICertificate{}, MissingCertificateError{certificateType, teamID}
		}
	}

	// only for debugging
	if isDebugLog {
		if err := LogAllAPICertificates(client, typeToLocalCerts); err != nil {
			log.Debugf("Failed to log all Developer Portal certificates: %s", err)
		}
	}

	validAPICertificates := map[appstoreconnect.CertificateType][]devportal.APICertificate{}
	for certificateType, validLocalCertificates := range typeToLocalCerts {
		matchingCertificates, err := MatchLocalToAPICertificates(client, certificateType, validLocalCertificates)
		if err != nil {
			return nil, err
		}

		if len(matchingCertificates) > 0 {
			log.Debugf("Certificates type %s has matches on Developer Portal:", certificateType)
			for _, cert := range matchingCertificates {
				log.Debugf("- %s", cert.Certificate)
			}
		}

		if requiredCertificateTypes[certificateType] && len(matchingCertificates) == 0 {
			return nil, fmt.Errorf("not found any of the following %s certificates on Developer Portal:\n%s", certificateType, CertsToString(validLocalCertificates))
		}

		if len(matchingCertificates) > 0 {
			validAPICertificates[certificateType] = matchingCertificates
		}
	}

	return validAPICertificates, nil
}

// GetValidLocalCertificates returns validated and deduplicated local certificates
func GetValidLocalCertificates(certificates []certificateutil.CertificateInfoModel, teamID string) (map[appstoreconnect.CertificateType][]certificateutil.CertificateInfoModel, error) {
	preFilteredCerts := certificateutil.FilterValidCertificateInfos(certificates)

	if len(preFilteredCerts.InvalidCertificates) != 0 {
		log.Warnf("Ignoring expired or not yet valid certificates: %s", preFilteredCerts.InvalidCertificates)
	}
	if len(preFilteredCerts.DuplicatedCertificates) != 0 {
		log.Warnf("Ignoring duplicated certificates with the same name: %s", preFilteredCerts.DuplicatedCertificates)
	}

	log.Debugf("Valid and deduplicated certificates:\n%s", CertsToString(preFilteredCerts.ValidCertificates))

	localCertificates := map[appstoreconnect.CertificateType][]certificateutil.CertificateInfoModel{}
	for _, certType := range []appstoreconnect.CertificateType{appstoreconnect.IOSDevelopment, appstoreconnect.IOSDistribution} {
		localCertificates[certType] = filterCertificates(preFilteredCerts.ValidCertificates, certType, teamID)
	}

	log.Debugf("Valid and deduplicated certificates for Development team (%s):\n%s", teamID, CertsToString(preFilteredCerts.ValidCertificates))

	return localCertificates, nil
}

// MatchLocalToAPICertificates ...
func MatchLocalToAPICertificates(client devportal.CertificateSource, certificateType appstoreconnect.CertificateType, localCertificates []certificateutil.CertificateInfoModel) ([]devportal.APICertificate, error) {
	var matchingCertificates []devportal.APICertificate

	for _, localCert := range localCertificates {
		cert, err := client.QueryCertificateBySerial(localCert.Certificate.SerialNumber)
		if err != nil {
			log.Warnf("Certificate (%s) not found on Developer Portal: %s", localCert, err)
			continue
		}
		cert.Certificate = localCert

		log.Debugf("Certificate (%s) found with ID: %s", localCert, cert.ID)

		matchingCertificates = append(matchingCertificates, cert)
	}

	return matchingCertificates, nil
}

// LogAllAPICertificates ...
func LogAllAPICertificates(client devportal.CertificateSource, localCertificates map[appstoreconnect.CertificateType][]certificateutil.CertificateInfoModel) error {
	certificates, err := client.QueryAllIOSCertificates()
	if err != nil {
		return fmt.Errorf("failed to query certificates on Developer Portal: %s", err)
	}

	for certType, certs := range certificates {
		log.Debugf("Developer Portal %s certificates:", certType)
		for _, cert := range certs {
			log.Debugf("- %s", cert.Certificate)
		}
	}

	return nil
}

// filterCertificates returns the certificates matching to the given common name, developer team ID, and distribution type.
func filterCertificates(certificates []certificateutil.CertificateInfoModel, certificateType appstoreconnect.CertificateType, teamID string) []certificateutil.CertificateInfoModel {
	// filter by distribution type
	var filteredCertificates []certificateutil.CertificateInfoModel
	for _, certificate := range certificates {
		if certificateType == appstoreconnect.IOSDistribution && isDistributionCertificate(certificate) {
			filteredCertificates = append(filteredCertificates, certificate)
		} else if certificateType == appstoreconnect.IOSDevelopment && !isDistributionCertificate(certificate) {
			filteredCertificates = append(filteredCertificates, certificate)
		}
	}

	log.Debugf("Valid certificates with type %s:\n%s", certificateType, CertsToString(filteredCertificates))

	if len(filteredCertificates) == 0 {
		return nil
	}

	// filter by team
	if teamID != "" {
		certsByTeam := mapCertsToTeams(filteredCertificates)
		filteredCertificates = certsByTeam[teamID]
	}

	log.Debugf("Valid certificates with type %s, Team ID: (%s):\n%s", certificateType, teamID, CertsToString(filteredCertificates))

	if len(filteredCertificates) == 0 {
		return nil
	}

	log.Debugf("Valid certificates with type %s, Team ID: (%s)\n%s ", certificateType, teamID, CertsToString(filteredCertificates))

	return filteredCertificates
}

func mapCertsToTeams(certs []certificateutil.CertificateInfoModel) map[string][]certificateutil.CertificateInfoModel {
	m := map[string][]certificateutil.CertificateInfoModel{}
	for _, c := range certs {
		teamCerts := m[c.TeamID]
		m[c.TeamID] = append(teamCerts, c)
	}
	return m
}

func isDistributionCertificate(cert certificateutil.CertificateInfoModel) bool {
	// Apple certificate types: https://help.apple.com/xcode/mac/current/#/dev80c6204ec)
	return strings.HasPrefix(strings.ToLower(cert.CommonName), strings.ToLower("iPhone Distribution")) ||
		strings.HasPrefix(strings.ToLower(cert.CommonName), strings.ToLower("Apple Distribution"))
}
