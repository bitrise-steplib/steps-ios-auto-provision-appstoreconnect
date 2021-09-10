package spaceship

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/bitrise-io/go-steputils/command/rubycommand"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/appleauth"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

type Client struct {
	workDir    string
	authConfig appleauth.AppleID
}

func NewClient(authConfig *appleauth.AppleID) (*Client, error) {
	if authConfig == nil {
		panic("Invalid authentication state")
	}

	dir, err := getSpaceshipDirectory()
	if err != nil {
		return nil, err
	}

	return &Client{
		workDir:    dir,
		authConfig: *authConfig,
	}, nil
}

func NewSpaceshipDevportalClient(client *Client) autoprovision.DevportalClient {
	return autoprovision.DevportalClient{
		CertificateSource: NewSpaceshipCertificateSource(client),
		DeviceLister:      &SpaceshipDeviceLister{},
		ProfileClient:     NewSpaceshipProfileClient(client),
	}
}

func (c *Client) createRequestCommand(subCommand string, opts ...string) (*command.Model, error) {
	s := []string{"bundle", "exec", "ruby", "main.rb",
		"--username", c.authConfig.Username,
		"--password", c.authConfig.Password,
		"--subcommand", subCommand,
	}
	s = append(s, opts...)
	spaceshipCmd, err := rubycommand.NewFromSlice(s)
	if err != nil {
		return nil, err
	}
	spaceshipCmd.SetDir(c.workDir)

	var envs []string
	var globallySetAuthEnvs []string
	for envKey, envValue := range fastlaneAuthParams(c.authConfig) {
		if _, set := os.LookupEnv(envKey); set {
			globallySetAuthEnvs = append(globallySetAuthEnvs, envKey)
		}

		envs = append(envs, fmt.Sprintf("%s=%s", envKey, envValue))
	}
	if len(globallySetAuthEnvs) != 0 {
		log.Warnf("Fastlane authentication-related environment varibale(s) (%s) are set, overriding.", globallySetAuthEnvs)
		log.Infof("To stop overriding authentication-related environment variables, please set Bitrise Apple Developer Connection input to 'off' and leave authentication-related inputs empty.")
	}

	spaceshipCmd.AppendEnvs(envs...)

	return spaceshipCmd, nil
}

// fastlaneAuthParams converts Apple credentials to Fastlane env vars and arguments
func fastlaneAuthParams(appleID appleauth.AppleID) map[string]string {
	envs := make(map[string]string)
	if appleID.Session != "" {
		envs["FASTLANE_SESSION"] = appleID.Session
	}
	if appleID.AppSpecificPassword != "" {
		envs["FASTLANE_APPLE_APPLICATION_SPECIFIC_PASSWORD"] = appleID.AppSpecificPassword
	}

	return envs
}

func runSpaceshipCommand(cmd *command.Model) (string, error) {
	var output bytes.Buffer
	outWriter := io.MultiWriter(os.Stdout, &output)
	cmd.SetStdout(outWriter)
	cmd.SetStderr(outWriter)

	// ToDo: redact password
	fmt.Println()
	log.Donef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("spaceship command failed, output: %s, error: %v", output.String(), err)
	}

	jsonRegexp := regexp.MustCompile(`(?m)^\{.*\}$`)
	match := jsonRegexp.FindString(output.String())
	if match == "" {
		return "", fmt.Errorf("output does not contain response: %s", output.String())
	}

	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(match), &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %v (%s)", err, match)
	}

	if response.Error != "" {
		return "", fmt.Errorf("failed to query Developer Portal: %s", response.Error)
	}

	return match, nil
}
