package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/log"
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

	certURLs, err := cfg.CertificateFileURLs()
	if err != nil {
		failf("Failed to convert certificate URLs: %s", err)
	}

	authClientType, err := parseAuthSources(cfg.BitriseConnection)
	if err != nil {
		failf("Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", cfg.BitriseConnection)
	}

	//
	// Auto codesign
	f := devportalclient.NewClientFactory()
	connection, err := f.CreateBitriseConnection(cfg.BuildURL, cfg.BuildAPIToken)
	if err != nil {
		failf(err.Error())
	}

	devPortalClient, err := f.CreateClient(authClientType, cfg.TeamID, connection)
	if err != nil {
		failf(err.Error())
	}

	keychain, err := keychain.New(cfg.KeychainPath, cfg.KeychainPassword, command.NewFactory(env.NewRepository()))
	if err != nil {
		panic(fmt.Sprintf("failed to initialize keychain: %s", err))
	}

	certDownloader := certdownloder.NewDownloader(certURLs)
	manager := autocodesign.NewCodesignAssetManager(devPortalClient, certDownloader, codesignasset.NewWriter(*keychain))

	// Analyzing project
	fmt.Println()
	log.Infof("Analyzing project")
	project, err := projectmanager.NewProject(cfg.ProjectPath, cfg.Scheme, cfg.Configuration)
	if err != nil {
		failf(err.Error())
	}

	appLayout, err := project.GetAppLayout(true)
	if err != nil {
		failf(err.Error())
	}

	distribution := cfg.DistributionType()
	codesignAssetsByDistributionType, err := manager.EnsureCodesignAssets(appLayout, autocodesign.CodesignAssetsOpts{
		DistributionType:       distribution,
		BitriseTestDevices:     connection.TestDevices,
		MinProfileValidityDays: cfg.MinProfileDaysValid,
		VerboseLog:             cfg.VerboseLog,
	})
	if err != nil {
		failf(fmt.Sprintf("Automatic code signing failed: %s", err))
	}

	if err := project.ForceCodesignAssets(distribution, codesignAssetsByDistributionType); err != nil {
		failf(fmt.Sprintf("Failed to force codesign settings: %s", err))
	}

	//
	// Export output
	fmt.Println()
	log.Infof("Exporting outputs")

	teamID := codesignAssetsByDistributionType[distribution].Certificate.TeamID
	outputs := map[string]string{
		"BITRISE_EXPORT_METHOD":  cfg.Distribution,
		"BITRISE_DEVELOPER_TEAM": teamID,
	}

	settings, ok := codesignAssetsByDistributionType[autocodesign.Development]
	if ok {
		outputs["BITRISE_DEVELOPMENT_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := project.MainTargetBundleID()
		if err != nil {
			failf("Failed to read bundle ID for the main target: %s", err)
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf("No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_DEVELOPMENT_PROFILE"] = profile.Attributes().UUID
	}

	if distribution != autocodesign.Development {
		settings, ok := codesignAssetsByDistributionType[distribution]
		if !ok {
			failf("No codesign settings ensured for the selected distribution type: %s", distribution)
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
