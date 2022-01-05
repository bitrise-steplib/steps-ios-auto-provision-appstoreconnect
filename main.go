package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	v1log "github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-io/go-xcode/v2/autocodesign"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/certdownloader"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/codesignasset"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/devportalclient"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/keychain"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/localcodesignasset"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/projectmanager"
)

func failf(logger log.Logger, format string, args ...interface{}) {
	logger.Errorf(format, args...)
	os.Exit(1)
}

func main() {
	var logger = log.NewLogger()

	// Parse and validate inputs
	var cfg Config
	parser := stepconf.NewInputParser(env.NewRepository())
	if err := parser.Parse(&cfg); err != nil {
		failf(logger, "Config: %s", err)
	}
	stepconf.Print(cfg)
	logger.EnableDebugLog(cfg.VerboseLog)
	v1log.SetEnableDebugLog(cfg.VerboseLog) // for compatibility

	certsWithPrivateKey, err := cfg.Certificates()
	if err != nil {
		failf(logger, "Failed to convert certificate URLs: %s", err)
	}

	authInputs := appleauth.Inputs{
		APIIssuer:  cfg.APIIssuer,
		APIKeyPath: string(cfg.APIKeyPath),
	}
	if err := authInputs.Validate(); err != nil {
		failf(logger, "Issue with authentication related inputs: %v", err)
	}

	// Analyze project
	fmt.Println()
	logger.Infof("Analyzing project")
	project, err := projectmanager.NewProject(projectmanager.InitParams{
		ProjectOrWorkspacePath: cfg.ProjectPath,
		SchemeName:             cfg.Scheme,
		ConfigurationName:      cfg.Configuration,
	})
	if err != nil {
		failf(logger, err.Error())
	}

	appLayout, err := project.GetAppLayout(cfg.SignUITestTargets)
	if err != nil {
		failf(logger, err.Error())
	}

	authSources, err := parseAuthSources(cfg.BitriseConnection)
	if err != nil {
		failf(logger, "Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", cfg.BitriseConnection)
	}

	var connection *devportalservice.AppleDeveloperConnection
	isRunningOnBitrise := cfg.BuildURL != "" && cfg.BuildAPIToken != ""

	switch {
	case cfg.BitriseConnection != "off" && !isRunningOnBitrise:
		fmt.Println()
		logger.Warnf("Connected Apple Developer Portal Account not found. Step is not running on bitrise.io: BITRISE_BUILD_URL and BITRISE_BUILD_API_TOKEN envs are not set")
	case cfg.BitriseConnection != "off":
		f := devportalclient.NewFactory(logger)
		c, err := f.CreateBitriseConnection(cfg.BuildURL, cfg.BuildAPIToken)
		if err != nil {
			failf(logger, err.Error())
		}

		connection = c
	}

	devPortalClient, err := createClient(authSources, authInputs, cfg.TeamID, connection)
	if err != nil {
		failf(logger, err.Error())
	}

	// Create codesign manager
	keychain, err := keychain.New(cfg.KeychainPath, cfg.KeychainPassword, command.NewFactory(env.NewRepository()))
	if err != nil {
		failf(logger, fmt.Sprintf("failed to initialize keychain: %s", err))
	}

	certDownloader := certdownloader.NewDownloader(certsWithPrivateKey, retry.NewHTTPClient().StandardClient())
	localCodeSignAssetManager := localcodesignasset.NewManager(localcodesignasset.NewProvisioningProfileProvider(), localcodesignasset.NewProvisioningProfileConverter())
	manager := autocodesign.NewCodesignAssetManager(devPortalClient, certDownloader, codesignasset.NewWriter(*keychain), localCodeSignAssetManager)

	// Auto codesign
	distribution := cfg.DistributionType()
	var testDevices []devportalservice.TestDevice
	if cfg.RegisterTestDevices && connection != nil {
		testDevices = connection.TestDevices
	}
	codesignAssetsByDistributionType, err := manager.EnsureCodesignAssets(appLayout, autocodesign.CodesignAssetsOpts{
		DistributionType:       distribution,
		BitriseTestDevices:     testDevices,
		MinProfileValidityDays: cfg.MinProfileDaysValid,
		VerboseLog:             cfg.VerboseLog,
	})
	if err != nil {
		failf(logger, fmt.Sprintf("Automatic code signing failed: %s", err))
	}

	if err := project.ForceCodesignAssets(distribution, codesignAssetsByDistributionType); err != nil {
		failf(logger, fmt.Sprintf("Failed to force codesign settings: %s", err))
	}

	// Export output
	fmt.Println()
	logger.Infof("Exporting outputs")

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
			failf(logger, "Failed to read bundle ID for the main target: %s", err)
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf(logger, "No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_DEVELOPMENT_PROFILE"] = profile.Attributes().UUID
	}

	if distribution != autocodesign.Development {
		settings, ok := codesignAssetsByDistributionType[distribution]
		if !ok {
			failf(logger, "No codesign settings ensured for the selected distribution type: %s", distribution)
		}

		outputs["BITRISE_PRODUCTION_CODESIGN_IDENTITY"] = settings.Certificate.CommonName

		bundleID, err := project.MainTargetBundleID()
		if err != nil {
			failf(logger, err.Error())
		}
		profile, ok := settings.ArchivableTargetProfilesByBundleID[bundleID]
		if !ok {
			failf(logger, "No provisioning profile ensured for the main target")
		}

		outputs["BITRISE_PRODUCTION_PROFILE"] = profile.Attributes().UUID
	}

	for k, v := range outputs {
		logger.Donef("%s=%s", k, v)
		if err := tools.ExportEnvironmentWithEnvman(k, v); err != nil {
			failf(logger, "Failed to export %s=%s: %s", k, v, err)
		}
	}
}
