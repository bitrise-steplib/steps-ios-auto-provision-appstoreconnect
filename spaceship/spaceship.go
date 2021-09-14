package spaceship

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

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
		DeviceClient:      NewDeviceClient(client),
		ProfileClient:     NewSpaceshipProfileClient(client),
	}
}

type spaceshipCommand struct {
	command              *command.Model
	printableCommandArgs string
}

func (c *Client) createRequestCommand(subCommand string, opts ...string) (spaceshipCommand, error) {
	authParams := []string{
		"--username", c.authConfig.Username,
		"--password", c.authConfig.Password,
		"--session", base64.StdEncoding.EncodeToString([]byte(c.authConfig.Session)),
		"--team-id", c.teamID,
	}
	s := []string{"bundle", "exec", "ruby", "main.rb",
		"--subcommand", subCommand,
	}
	s = append(s, opts...)
	printableCommand := strings.Join(s, " ")
	s = append(s, authParams...)

	spaceshipCmd, err := rubycommand.NewFromSlice(s)
	if err != nil {
		return spaceshipCommand{}, err
	}
	spaceshipCmd.SetDir(c.workDir)

	return spaceshipCommand{
		command:              spaceshipCmd,
		printableCommandArgs: printableCommand,
	}, nil
}

func runSpaceshipCommand(cmd spaceshipCommand) (string, error) {
	var output bytes.Buffer
	outWriter := &output
	cmd.command.SetStdout(outWriter)
	cmd.command.SetStderr(outWriter)

	log.Debugf("$ %s", cmd.printableCommandArgs)
	if err := cmd.command.Run(); err != nil {
		return "", fmt.Errorf("spaceship command failed, output: %s, error: %v", output.String(), err)
	}

	log.Debugf("\n%s\n", output.String())

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
