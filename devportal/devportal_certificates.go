package devportal

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/bitrise-io/go-steputils/command/gems"
	"github.com/bitrise-io/go-steputils/command/rubycommand"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/certificateutil"
)

//go:embed spaceship
var spaceship embed.FS

type certificates struct {
	Error string   `json:"error"`
	Data  []string `josn:"data"`
}

func GetAllCertificates() ([]certificateutil.CertificateInfoModel, error) {
	spaceshipDir, err := getSpaceshipDirectory()
	if err != nil {
		return nil, err
	}

	cmd, err := getSpaceshipCommand(spaceshipDir, "certificates")
	if err != nil {
		return nil, err
	}

	output, err := runSpaceshipCommand(cmd)
	if err != nil {
		return nil, err
	}

	var certsResponse certificates
	if err := json.Unmarshal([]byte(output), &certsResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	if certsResponse.Error != "" {
		return nil, fmt.Errorf("could not query devportal: %v", err)
	}

	var certs []certificateutil.CertificateInfoModel
	for _, encodedCert := range certsResponse.Data {
		pemContent, err := base64.StdEncoding.DecodeString(encodedCert)
		if err != nil {
			return nil, err
		}

		certInfo, err := certificateutil.CeritifcateFromPemContent(pemContent)
		if err != nil {
			return nil, err
		}

		log.Infof("cert: %v", certInfo)
		// certs = append(certs, certInfo)
	}

	return certs, nil
}

func getSpaceshipDirectory() (string, error) {
	targetDir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}

	fsys, err := fs.Sub(spaceship, "spaceship")
	if err != nil {
		return "", err
	}

	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Warnf("%s", err)
			return err
		}

		log.Printf("%s", path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(targetDir, path), 0700)
		}

		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		if err := os.WriteFile(filepath.Join(targetDir, path), content, 0700); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return "", err
	}

	bundler := gems.Version{Found: true, Version: "2.2.24"}
	installBundlerCommand := gems.InstallBundlerCommand(bundler)
	installBundlerCommand.SetStdout(os.Stdout).SetStderr(os.Stderr)
	installBundlerCommand.SetDir(targetDir)

	fmt.Println()
	log.Donef("$ %s", installBundlerCommand.PrintableCommandArgs())
	if err := installBundlerCommand.Run(); err != nil {
		return "", fmt.Errorf("command failed, error: %s", err)
	}

	fmt.Println()
	cmd, err := gems.BundleInstallCommand(bundler)
	if err != nil {
		return "", fmt.Errorf("failed to create bundle command model, error: %s", err)
	}
	cmd.SetStdout(os.Stdout).SetStderr(os.Stderr)
	cmd.SetDir(targetDir)

	fmt.Println()
	log.Donef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("Command failed, error: %s", err)
	}

	return targetDir, nil
}

func getSpaceshipCommand(targetDir, subCommand string) (*command.Model, error) {
	spaceshipCmd, err := rubycommand.NewFromSlice([]string{"bundle", "exec", "ruby", "main.rb", subCommand})
	if err != nil {
		return nil, err
	}
	spaceshipCmd.SetDir(targetDir)

	return spaceshipCmd, nil
}

func runSpaceshipCommand(cmd *command.Model) (string, error) {
	fmt.Println()
	log.Donef("$ %s", cmd.PrintableCommandArgs())
	output, err := cmd.RunAndReturnTrimmedOutput()
	if err != nil {
		return "", fmt.Errorf("spaceship command failed: %s", output)
	}

	jsonRegexp := regexp.MustCompile(`\n\{.*\}$`)
	match := jsonRegexp.FindString(output)
	if match == "" {
		return "", fmt.Errorf("output does not contain response: %s", output)
	}

	return match, nil
}
