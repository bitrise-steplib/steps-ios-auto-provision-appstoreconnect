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

func failf(format string, args ...interface{}) {
	v1log.Errorf(format, args...)
	os.Exit(1)
}

func main() {
	// Parse and validate inputs
	var cfg Config
	parser := stepconf.NewInputParser(env.NewRepository())
	if err := parser.Parse(&cfg); err != nil {
		failf("Config: %s", err)
	}
	stepconf.Print(cfg)

	var logger = log.NewLogger()
	logger.EnableDebugLog(cfg.VerboseLog)
	v1log.SetEnableDebugLog(cfg.VerboseLog) // for compatibility

	logger.Warnf(`
This Step has been deprecated in the favour of the new automatic code signing options on Bitrise.

Option A)
The latest versions of the [Xcode Archive & Export for iOS](https://www.bitrise.io/integrations/steps/xcode-archive),
[Xcode Build for testing for iOS](https://www.bitrise.io/integrations/steps/xcode-build-for-test),
and the [Export iOS and tvOS Xcode archive](https://www.bitrise.io/integrations/steps/xcode-archive) Steps
have built-in automatic code signing.
We suggest to remove this step from your workflow and start to use the automatic code signing feature in the Steps mentioned above.

Option B)
If you are not using any of the mentioned Xcode steps, then you can replace this iOS Auto Provision Step with
the [Manage iOS Code signing](https://www.bitrise.io/integrations/steps/manage-ios-code-signing) Step.

You can read more about these changes in our blog post:
https://blog.bitrise.io/post/simplifying-automatic-code-signing-on-bitrise
`)

	certsWithPrivateKey, err := cfg.Certificates()
	if err != nil {
		failf("Failed to convert certificate URLs: %s", err)
	}

	authInputs := appleauth.Inputs{
		APIIssuer:  cfg.APIIssuer,
		APIKeyPath: string(cfg.APIKeyPath),
	}
	if err := authInputs.Validate(); err != nil {
		failf("Issue with authentication related inputs: %v", err)
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
		failf(err.Error())
	}

	appLayout, err := project.GetAppLayout(cfg.SignUITestTargets)
	if err != nil {
		failf(err.Error())
	}

	authSources, err := parseAuthSources(cfg.BitriseConnection)
	if err != nil {
		failf("Invalid input: unexpected value for Bitrise Apple Developer Connection (%s)", cfg.BitriseConnection)
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
			failf(err.Error())
		}

		connection = c
	}

	devPortalClient, err := createClient(authSources, authInputs, cfg.TeamID, connection)
	if err != nil {
		failf(err.Error())
	}

	// Create codesign manager
	keychain, err := keychain.New(cfg.KeychainPath, cfg.KeychainPassword, command.NewFactory(env.NewRepository()))
	if err != nil {
		failf(fmt.Sprintf("failed to initialize keychain: %s", err))
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
		failf(fmt.Sprintf("Automatic code signing failed: %s", err))
	}

	if err := project.ForceCodesignAssets(distribution, codesignAssetsByDistributionType); err != nil {
		failf(fmt.Sprintf("Failed to force codesign settings: %s", err))
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
		logger.Donef("%s=%s", k, v)
		if err := tools.ExportEnvironmentWithEnvman(k, v); err != nil {
			failf("Failed to export %s=%s: %s", k, v, err)
		}
	}
}
