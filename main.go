package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/keychain"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/spaceship"
)

// downloadCertificates downloads and parses a list of p12 files
func downloadCertificates(URLs []CertificateFileURL) ([]certificateutil.CertificateInfoModel, error) {
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}
	var certInfos []certificateutil.CertificateInfoModel

	for i, p12 := range URLs {
		log.Debugf("Downloading p12 file number %d from %s", i, p12.URL)

		p12CertInfos, err := downloadPKCS12(httpClient, p12.URL, p12.Passphrase)
		if err != nil {
			return nil, err
		}
		log.Debugf("Codesign identities included:\n%s", autoprovision.CertsToString(p12CertInfos))

		certInfos = append(certInfos, p12CertInfos...)
	}

	return certInfos, nil
}

// downloadPKCS12 downloads a pkcs12 format file and parses certificates and matching private keys.
func downloadPKCS12(httpClient *http.Client, certificateURL, passphrase string) ([]certificateutil.CertificateInfoModel, error) {
	contents, err := downloadFile(httpClient, certificateURL)
	if err != nil {
		return nil, err
	} else if contents == nil {
		return nil, fmt.Errorf("certificate (%s) is empty", certificateURL)
	}

	infos, err := certificateutil.CertificatesFromPKCS12Content(contents, passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate (%s), err: %s", certificateURL, err)
	}

	return infos, nil
}

func downloadFile(httpClient *http.Client, src string) ([]byte, error) {
	url, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url (%s): %s", src, err)
	}

	// Local file
	if url.Scheme == "file" {
		src := strings.Replace(src, url.Scheme+"://", "", -1)

		return ioutil.ReadFile(src)
	}

	// Remote file
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %s", err)
	}

	var contents []byte
	err = retry.Times(2).Wait(5 * time.Second).Try(func(attempt uint) error {
		log.Debugf("Downloading %s, attempt %d", src, attempt)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download (%s): %s", src, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Warnf("failed to close (%s) body: %s", src, err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download (%s) failed with status code (%d)", src, resp.StatusCode)
		}

		contents, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response (%s): %s", src, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return contents, nil
}

func failf(format string, args ...interface{}) {
	log.Errorf(format, args...)
	os.Exit(1)
}

const notConnected = `Connected Apple Developer Portal Account not found.
Most likely because there is no Apple Developer Portal Account connected to the build.
Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/`

func handleSessionDataError(err error) {
	if err == nil {
		return
	}

	if networkErr, ok := err.(devportalservice.NetworkError); ok && networkErr.Status == http.StatusUnauthorized {
		fmt.Println()
		log.Warnf("%s", "Unauthorized to query Connected Apple Developer Portal Account. This happens by design, with a public app's PR build, to protect secrets.")

		return
	}

	fmt.Println()
	log.Errorf("Failed to activate Bitrise Apple Developer Portal connection: %s", err)
	log.Warnf("Read more: https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/")
}

func main() {
	var stepConf Config
	if err := stepconf.Parse(&stepConf); err != nil {
		failf("Config: %s", err)
	}
	stepconf.Print(stepConf)

	log.SetEnableDebugLog(stepConf.VerboseLog)

	// Creating AppstoreConnectAPI client
	fmt.Println()
	log.Infof("Creating AppstoreConnectAPI client")

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

	var connectionProvider *devportalservice.BitriseClient
	if stepConf.BuildURL != "" && stepConf.BuildAPIToken != "" {
		connectionProvider = devportalservice.NewBitriseClient(retry.NewHTTPClient().StandardClient(), stepConf.BuildURL, stepConf.BuildAPIToken)
	} else {
		fmt.Println()
		log.Warnf("Connected Apple Developer Portal Account not found. Step is not running on bitrise.io: BITRISE_BUILD_URL and BITRISE_BUILD_API_TOKEN envs are not set")
	}
	var conn *devportalservice.AppleDeveloperConnection
	if stepConf.BitriseConnection != "off" && connectionProvider != nil {
		var err error
		conn, err = connectionProvider.GetAppleDeveloperConnection()
		if err != nil {
			handleSessionDataError(err)
		}

		if conn != nil && len(conn.DuplicatedTestDevices) != 0 {
			log.Debugf("Devices with duplicated UDID are registered on Bitrise, will be ignored:")
			for _, d := range conn.DuplicatedTestDevices {
				log.Debugf("- %s, %s, UDID (%s), added at %s", d.Title, d.DeviceType, d.DeviceID, d.UpdatedAt)
			}
		}

		if conn != nil && (conn.APIKeyConnection == nil) {
			fmt.Println()
			log.Warnf("%s", notConnected)
		}
	}

	authConfig, err := appleauth.Select(conn, authSources, authInputs)
	if err != nil {
		failf("Could not configure Apple Service authentication: %v", err)
	}

	var devportalClient autoprovision.DevportalClient
	if authConfig.APIKey != nil {
		httpClient := appstoreconnect.NewRetryableHTTPClient()
		client := appstoreconnect.NewClient(httpClient, authConfig.APIKey.KeyID, authConfig.APIKey.IssuerID, []byte(authConfig.APIKey.PrivateKey))
		client.EnableDebugLogs = false // Turn off client debug logs including HTTP call debug logs
		log.Donef("the client created for %s", client.BaseURL)
		devportalClient = autoprovision.NewAPIDevportalClient(client)
	} else if authConfig.AppleID != nil {
		client, err := spaceship.NewClient(*authConfig.AppleID, stepConf.TeamID)
		if err != nil {
			failf("failed to initialize Spaceship client: %v")
		}
		devportalClient = spaceship.NewSpaceshipDevportalClient(client)
	} else {
		panic("No Apple authentication credentials found.")
	}

	// Analyzing project
	projectSettings := autoprovision.ProjectSettings{
		ProjectPath:       stepConf.ProjectPath,
		Scheme:            stepConf.Scheme,
		Configuration:     stepConf.Configuration,
		SignUITestTargets: stepConf.SignUITestTargets,
	}
	codesignRequirements, config, err := autoprovision.GetCodesignSettingsFromProject(projectSettings)
	if err != nil {
		failf("%v", err)
	}

	// Downloading certificates
	fmt.Println()
	log.Infof("Downloading certificates")

	certURLs, err := stepConf.CertificateFileURLs()
	if err != nil {
		failf("Failed to convert certificate URLs: %s", err)
	}

	certs, err := downloadCertificates(certURLs)
	if err != nil {
		failf("Failed to download certificates: %s", err)
	}

	log.Printf("%d certificates downloaded:", len(certs))

	for _, cert := range certs {
		log.Printf("- %s", cert.CommonName)
	}

	certsByType, distrTypes, err := autoprovision.SelectCertificatesAndDistributionTypes(
		devportalClient.CertificateSource,
		certs,
		stepConf.DistributionType(),
		codesignRequirements.TeamID,
		stepConf.SignUITestTargets,
		stepConf.VerboseLog,
	)
	if err != nil {
		failf("%v", err)
	}

	// Ensure devices
	var devPortalDeviceIDs []string

	var bitriseTestDevices []devportalservice.TestDevice
	if conn != nil {
		bitriseTestDevices = conn.TestDevices
	}

	if autoprovision.DistributionTypeRequiresDeviceList(distrTypes) {
		var err error
		devPortalDeviceIDs, err = autoprovision.EnsureTestDevices(devportalClient.DeviceClient, bitriseTestDevices, codesignRequirements.Platform)
		if err != nil {
			failf("Failed to ensure test devices: %s", err)
		}
	}

	// Ensure Profiles
	codesignSettingsByDistributionType, err := autoprovision.EnsureProfiles(devportalClient.ProfileClient, distrTypes, certsByType, codesignRequirements, devPortalDeviceIDs, stepConf.MinProfileDaysValid)
	if err != nil {
		failf("Failed to ensure profiles: %s", err)
	}

	// Force Codesign Settings
	if err = autoprovision.ForceCodesignSettings(projectSettings, stepConf.DistributionType(), codesignSettingsByDistributionType); err != nil {
		failf("Failed to force codesign settings: %s", err)
	}

	// Install certificates and profiles
	fmt.Println()
	log.Infof("Install certificates and profiles")

	kc, err := keychain.New(stepConf.KeychainPath, stepConf.KeychainPassword)
	if err != nil {
		failf("Failed to initialize keychain: %s", err)
	}

	i := 0
	for _, codesignSettings := range codesignSettingsByDistributionType {
		log.Printf("certificate: %s", codesignSettings.Certificate.CommonName)

		if err := kc.InstallCertificate(codesignSettings.Certificate, ""); err != nil {
			failf("Failed to install certificate: %s", err)
		}

		log.Printf("profiles:")
		for _, profile := range codesignSettings.ArchivableTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := autoprovision.WriteProfile(profile); err != nil {
				failf("Failed to write profile to file: %s", err)
			}
		}

		for _, profile := range codesignSettings.UITestTargetProfilesByBundleID {
			log.Printf("- %s", profile.Attributes().Name)

			if err := autoprovision.WriteProfile(profile); err != nil {
				failf("Failed to write profile to file: %s", err)
			}
		}

		if i < len(codesignSettingsByDistributionType)-1 {
			fmt.Println()
		}
		i++
	}

	// Export output
	fmt.Println()
	log.Infof("Exporting outputs")

	projHelper, _, err := autoprovision.NewProjectHelper(stepConf.ProjectPath, stepConf.Scheme, stepConf.Configuration)
	if err != nil {
		failf("Failed to analyze project: %s", err)
	}

	outputs := map[string]string{
		"BITRISE_EXPORT_METHOD":  stepConf.Distribution,
		"BITRISE_DEVELOPER_TEAM": codesignRequirements.TeamID,
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
