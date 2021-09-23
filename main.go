package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-io/go-xcode/autocodesign"
	"github.com/bitrise-io/go-xcode/autocodesign/certdownloder"
	"github.com/bitrise-io/go-xcode/autocodesign/codesignasset"
	"github.com/bitrise-io/go-xcode/autocodesign/devportalclient"
	"github.com/bitrise-io/go-xcode/autocodesign/keychain"
	"github.com/bitrise-io/go-xcode/autocodesign/projectmanager"
)

func failf(format string, args ...interface{}) {
	log.Errorf(format, args...)
	os.Exit(1)
}

func main() {
	var cfg Config
	parser := stepconf.NewInputParser(env.NewRepository())
	if err := parser.Parse(&cfg); err != nil {
		failf("Config: %s", err)
	}
	stepconf.Print(cfg)

	log.SetEnableDebugLog(cfg.VerboseLog)

	// Creating AppstoreConnectAPI client
	fmt.Println()
	log.Infof("Creating AppstoreConnectAPI client")

	authInputs := appleauth.Inputs{
		APIIssuer:  cfg.APIIssuer,
		APIKeyPath: string(cfg.APIKeyPath),
	}
	if err := authInputs.Validate(); err != nil {
		failf("Issue with authentication related inputs: %v", err)
	}

	authSources, err := parseAuthSources(cfg.BitriseConnection)
	if err != nil {
		failf("Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", cfg.BitriseConnection)
	}

	/////////
	f := devportalclient.NewClientFactory()
	connection, err := f.CreateBitriseConnection(cfg.BuildURL, cfg.BuildAPIToken)
	if err != nil {
		panic(err)
	}

	devPortalClient, err := f.CreateClient(devportalclient.AppleIDClient, "teamID", connection)
	if err != nil {
		panic(err)
	}

	keychain, err := keychain.New("kc path", "kc password", command.NewFactory(env.NewRepository()))
	if err != nil {
		panic(fmt.Sprintf("failed to initialize keychain: %s", err))
	}

	certDownloader := certdownloder.NewDownloader(nil)
	manager := autocodesign.NewCodesignAssetManager(devPortalClient, certDownloader, codesignasset.NewWriter(*keychain))

	// Analyzing project
	fmt.Println()
	log.Infof("Analyzing project")
	project, err := projectmanager.NewProject("path", "scheme", "config")
	if err != nil {
		panic(err.Error())
	}

	appLayout, err := project.GetAppLayout(true)
	if err != nil {
		panic(err.Error())
	}

	distribution := autocodesign.Development
	codesignAssetsByDistributionType, err := manager.EnsureCodesignAssets(appLayout, autocodesign.CodesignAssetsOpts{
		DistributionType:       distribution,
		BitriseTestDevices:     connection.TestDevices,
		MinProfileValidityDays: 0,
		VerboseLog:             true,
	})
	if err != nil {
		panic(fmt.Sprintf("Automatic code signing failed: %s", err))
	}

	if err := project.ForceCodesignAssets(distribution, codesignAssetsByDistributionType); err != nil {
		panic(fmt.Sprintf("Failed to force codesign settings: %s", err))
	}
	/////////

	// Export output
	fmt.Println()
	log.Infof("Exporting outputs")

	outputs := map[string]string{
		"BITRISE_EXPORT_METHOD":  stepConf.Distribution,
		"BITRISE_DEVELOPER_TEAM": teamID,
	}

	settings, ok := codesignSettingsByDistributionType[autoprovision.Development]
	if ok {
		outputs["BITRISE_DEVELOPMENT_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := projHelper.TargetBundleID(projHelper.MainTarget.Name, config)
		if err != nil {
			failf("Failed to read bundle ID for the main target: %s", err)
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_DEVELOPMENT_PROFILE"] = profile.Attributes().UUID
	}

	if stepConf.DistributionType() != autoprovision.Development {
		settings, ok := codesignSettingsByDistributionType[stepConf.DistributionType()]
		if !ok {
			failf("No codesign settings ensured for the selected distribution type: %s", stepConf.DistributionType())
		}

		outputs["BITRISE_PRODUCTION_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := projHelper.TargetBundleID(projHelper.MainTarget.Name, config)
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
