package autocodesign

import (
	"fmt"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/keychain"
)

// InstallCertificatesAndProfiles ...
func InstallCertificatesAndProfiles(codesignSettingsByDistributionType map[DistributionType]CodesignSettings, keychainPath string, keychainPassword stepconf.Secret) error {
	fmt.Println()
	log.Infof("Install certificates and profiles")

	kc, err := keychain.New(keychainPath, keychainPassword)
	if err != nil {
		return fmt.Errorf("failed to initialize keychain: %s", err)
	}

	i := 0
	for _, codesignSettings := range codesignSettingsByDistributionType {
		log.Printf("certificate: %s", codesignSettings.Certificate.CommonName)

		if err := kc.InstallCertificate(codesignSettings.Certificate, ""); err != nil {
			return fmt.Errorf("failed to install certificate: %s", err)
		}

		log.Printf("profiles:")
		for _, profile := range codesignSettings.ArchivableTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := writeProfile(profile); err != nil {
				return fmt.Errorf("failed to write profile to file: %s", err)
			}
		}

		for _, profile := range codesignSettings.UITestTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := writeProfile(profile); err != nil {
				return fmt.Errorf("failed to write profile to file: %s", err)
			}
		}

		if i < len(codesignSettingsByDistributionType)-1 {
			fmt.Println()
		}
		i++
	}

	return nil
}
