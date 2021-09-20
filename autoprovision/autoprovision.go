package autoprovision

import (
	"fmt"
	"math/big"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// APICertificate is certificate present on Apple App Store Connect API, could match a local certificate
type APICertificate struct {
	Certificate certificateutil.CertificateInfoModel
	ID          string
}

// CertificateSource ...
type CertificateSource interface {
	QueryCertificateBySerial(*big.Int) (APICertificate, error)
	QueryAllIOSCertificates() (map[appstoreconnect.CertificateType][]APICertificate, error)
}

// DevportalClient ...
type DevportalClient struct {
	CertificateSource CertificateSource
	DeviceClient      DeviceClient
	ProfileClient     ProfileClient
}

// NewAPIDevportalClient ...
func NewAPIDevportalClient(client *appstoreconnect.Client) DevportalClient {
	return DevportalClient{
		CertificateSource: NewAPICertificateSource(client),
		DeviceClient:      NewAPIDeviceClient(client),
		ProfileClient:     NewAPIProfileClient(client),
	}
}

type ProjectSettings struct {
	ProjectPath, Scheme, Configuration string
	SignUITestTargets                  bool
}

type CodesignRequirements struct {
	TeamID                                 string
	Platform                               Platform
	ArchivableTargetBundleIDToEntitlements map[string]serialized.Object
	UITestTargetBundleIDs                  []string
}

func GetCodesignSettingsFromProject(settings ProjectSettings) (CodesignRequirements, string, error) {
	fmt.Println()
	log.Infof("Analyzing project")

	projHelper, config, err := NewProjectHelper(settings.ProjectPath, settings.Scheme, settings.Configuration)
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("failed to analyze project: %s", err)
	}

	log.Printf("Configuration: %s", config)

	teamID, err := projHelper.ProjectTeamID(config)
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("failed to read project team ID: %s", err)
	}

	log.Printf("Project team ID: %s", teamID)

	platform, err := projHelper.Platform(config)
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("Failed to read project platform: %s", err)
	}

	log.Printf("Platform: %s", platform)

	log.Printf("Application and App Extension targets:")
	for _, target := range projHelper.ArchivableTargets() {
		log.Printf("- %s", target.Name)
	}
	if settings.SignUITestTargets {
		log.Printf("UITest targets:")
		for _, target := range projHelper.UITestTargets {
			log.Printf("- %s", target.Name)
		}
	}

	archivableTargetBundleIDToEntitlements, err := projHelper.ArchivableTargetBundleIDToEntitlements()
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("failed to read archivable targets' entitlements: %s", err)
	}

	if ok, entitlement, bundleID := CanGenerateProfileWithEntitlements(archivableTargetBundleIDToEntitlements); !ok {
		log.Errorf("Can not create profile with unsupported entitlement (%s) for the bundle ID %s, due to App Store Connect API limitations.", entitlement, bundleID)
		return CodesignRequirements{}, "", fmt.Errorf("please generate provisioning profile manually on Apple Developer Portal and use the Certificate and profile installer Step instead")
	}

	uiTestTargetBundleIDs, err := projHelper.UITestTargetBundleIDs()
	if err != nil {
		return CodesignRequirements{}, "", fmt.Errorf("Failed to read UITest targets' entitlements: %s", err)
	}

	return CodesignRequirements{
		TeamID:                                 teamID,
		Platform:                               platform,
		ArchivableTargetBundleIDToEntitlements: archivableTargetBundleIDToEntitlements,
		UITestTargetBundleIDs:                  uiTestTargetBundleIDs,
	}, config, nil
}

func SelectCertificatesAndDistributionTypes(certificateSource CertificateSource, certs []certificateutil.CertificateInfoModel, distribution DistributionType, teamID string, signUITestTargets bool, verboseLog bool) (map[appstoreconnect.CertificateType][]APICertificate, []DistributionType, error) {
	certType, ok := CertificateTypeByDistribution[distribution]
	if !ok {
		panic(fmt.Sprintf("no valid certificate provided for distribution type: %s", distribution))
	}

	distrTypes := []DistributionType{distribution}
	requiredCertTypes := map[appstoreconnect.CertificateType]bool{certType: true}
	if distribution != Development {
		distrTypes = append(distrTypes, Development)

		if signUITestTargets {
			log.Warnf("UITest target requires development code signing in addition to the specified %s code signing", distribution)
			requiredCertTypes[appstoreconnect.IOSDevelopment] = true
		} else {
			requiredCertTypes[appstoreconnect.IOSDevelopment] = false
		}
	}

	certsByType, err := GetValidCertificates(certs, certificateSource, requiredCertTypes, teamID, verboseLog)
	if err != nil {
		if missingCertErr, ok := err.(MissingCertificateError); ok {
			log.Errorf(err.Error())
			log.Warnf("Maybe you forgot to provide a(n) %s type certificate.", missingCertErr.Type)
			log.Warnf("Upload a %s type certificate (.p12) on the Code Signing tab of the Workflow Editor.", missingCertErr.Type)

			return nil, nil, fmt.Errorf("") // Move out
		}
		return nil, nil, fmt.Errorf("failed to get valid certificates: %s", err)
	}

	if len(certsByType) == 1 && distribution != Development {
		// remove development distribution if there is no development certificate uploaded
		distrTypes = []DistributionType{distribution}
	}
	log.Printf("ensuring codesigning files for distribution types: %s", distrTypes)

	return certsByType, distrTypes, nil
}
