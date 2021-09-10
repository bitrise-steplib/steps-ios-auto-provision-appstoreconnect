package spaceship

import (
	"bytes"
	"encoding/base64"
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

// Client ...
type Client struct {
	workDir    string
	authConfig appleauth.AppleID
	teamID     string
}

// NewClient ...
func NewClient(authConfig *appleauth.AppleID, teamID string) (*Client, error) {
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
		teamID:     teamID,
	}, nil
}

// NewSpaceshipDevportalClient ...
func NewSpaceshipDevportalClient(client *Client) autoprovision.DevportalClient {
	return autoprovision.DevportalClient{
		CertificateSource: NewSpaceshipCertificateSource(client),
		DeviceLister:      &DeviceLister{},
		ProfileClient:     NewSpaceshipProfileClient(client),
	}
}

func (c *Client) createRequestCommand(subCommand string, opts ...string) (*command.Model, error) {
	s := []string{"bundle", "exec", "ruby", "main.rb",
		"--username", c.authConfig.Username,
		"--password", c.authConfig.Password,
		"--session", base64.StdEncoding.EncodeToString([]byte(c.authConfig.Session)),
		"--team-id", c.teamID,
		"--subcommand", subCommand,
	}
	s = append(s, opts...)
	spaceshipCmd, err := rubycommand.NewFromSlice(s)
	if err != nil {
		return nil, err
	}
	spaceshipCmd.SetDir(c.workDir)

	return spaceshipCmd, nil
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
