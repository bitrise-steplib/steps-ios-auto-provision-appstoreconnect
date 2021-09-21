package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autocodesign"
)

func failf(format string, args ...interface{}) {
	log.Errorf(format, args...)
	os.Exit(1)
}

func main() {
	var stepConf Config
	if err := stepconf.Parse(&stepConf); err != nil {
		failf("Config: %s", err)
	}
	stepconf.Print(stepConf)

	log.SetEnableDebugLog(stepConf.VerboseLog)

	authInputs := appleauth.Inputs{
		APIIssuer:  stepConf.APIIssuer,
		APIKeyPath: string(stepConf.APIKeyPath),
	}
	if err := authInputs.Validate(); err != nil {
		failf("Issue with authentication related inputs: %v", err)
	}

	authSources, err := parseAuthSources(stepConf.BitriseConnection)
	if err != nil {
		failf("Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", stepConf.BitriseConnection)
	}

	certURLs, err := stepConf.CertificateFileURLs()
	if err != nil {
		failf("Failed to convert certificate URLs: %s", err)
	}

	// Analyzing project
	fmt.Println()
	log.Infof("Analyzing project")

	project, err := autocodesign.NewProject(stepConf.ProjectPath, stepConf.Scheme, stepConf.Configuration)
	if err != nil {
		failf(err.Error())
	}

	appLayout, err := project.GetAppLayoutFromProject(stepConf.SignUITestTargets)
	if err != nil {
		failf(err.Error())
	}

	if stepConf.TeamID != "" {
		appLayout.TeamID = stepConf.TeamID
	}

	manager, err := autocodesign.NewManager(stepConf.BuildURL, stepConf.BuildAPIToken, authSources, authInputs, appLayout.TeamID)
	if err != nil {
		failf(err.Error())
	}

	codesignAssetsByDistributionType, err := manager.AutoCodesign(
		stepConf.DistributionType(),
		appLayout,
		certURLs,
		stepConf.MinProfileDaysValid,
		autocodesign.KeychainCredentials{
			Path:     stepConf.KeychainPath,
			Password: stepConf.KeychainPassword,
		},
		stepConf.VerboseLog,
	)
	if err != nil {
		failf("Automatic code signing failed: %s", err)
	}

	// Force Codesign settings
	if err := project.ForceCodesignAssets(stepConf.DistributionType(), codesignAssetsByDistributionType); err != nil {
		failf("Failed to force codesign settings: %s", err)
	}

	// Export output
	fmt.Println()
	log.Infof("Exporting outputs")

	outputs := map[string]string{
		"BITRISE_EXPORT_METHOD":  stepConf.Distribution,
		"BITRISE_DEVELOPER_TEAM": appLayout.TeamID,
	}

	settings, ok := codesignAssetsByDistributionType[autocodesign.Development]
	if ok {
		outputs["BITRISE_DEVELOPMENT_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := project.MainTargetBundleID()
		if err != nil {
			failf(err.Error())
		}

		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_DEVELOPMENT_PROFILE"] = profile.Attributes().UUID
	}

	if stepConf.DistributionType() != autocodesign.Development {
		settings, ok := codesignAssetsByDistributionType[stepConf.DistributionType()]
		if !ok {
			failf("No codesign settings ensured for the selected distribution type: %s", stepConf.DistributionType())
		}

		outputs["BITRISE_PRODUCTION_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := project.MainTargetBundleID()
		if err != nil {
			failf(err.Error())
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_PRODUCTION_PROFILE"] = profile.Attributes().UUID
	}

	for k, v := range outputs {
		log.Donef("%s=%s", k, v)
		if err := tools.ExportEnvironmentWithEnvman(k, v); err != nil {
			failf("Failed to export %s=%s: %s", k, v, err)
		}
	}

}
