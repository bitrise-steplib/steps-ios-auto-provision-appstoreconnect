package spaceship

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"regexp"

	"github.com/bitrise-io/go-steputils/command/gems"
	"github.com/bitrise-io/go-steputils/command/rubycommand"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

//go:embed spaceship
var spaceship embed.FS

type SpaceshipCertificateSource struct {
	workDir      string
	certificates map[appstoreconnect.CertificateType][]autoprovision.APICertificate
}

func (s *SpaceshipCertificateSource) QueryCertificateBySerial(serial *big.Int) (autoprovision.APICertificate, error) {
	if s.certificates == nil {
		s.downloadAll()
	}

	for _, cert := range s.certificates[appstoreconnect.IOSDevelopment] {
		if serial.Cmp(cert.Certificate.Certificate.SerialNumber) == 0 {
			return cert, nil
		}
	}

	return autoprovision.APICertificate{}, fmt.Errorf("can not find certificate with serial")
}

func (s *SpaceshipCertificateSource) QueryAllIOSCertificates() (map[appstoreconnect.CertificateType][]autoprovision.APICertificate, error) {
	if s.certificates == nil {
		s.downloadAll()
	}

	return s.certificates, nil
}

func NewSpaceshipCertificateSource(client Client) autoprovision.CertificateSource {
	return &SpaceshipCertificateSource{
		workDir: client.workDir,
	}
}

func (s *SpaceshipCertificateSource) downloadAll() error {
	devCertsCmd, err := getSpaceshipCommand(s.workDir, "list_dev_certs")
	if err != nil {
		return err
	}

	// distCertsCommand, err := getSpaceshipCommand(spaceshipDir, "list_dist_certs")
	// if err != nil {
	// 	return nil, err
	// }

	devCerts, err := parseCertificates(devCertsCmd)
	if err != nil {
		return err
	}

	// distCers, err := parseCertificates(distCertsCommand)
	// if err != nil {
	// 	return nil, err
	// }

	s.certificates = map[appstoreconnect.CertificateType][]autoprovision.APICertificate{
		appstoreconnect.IOSDevelopment: devCerts,
	}

	return nil
}

type certificates struct {
	Error string `json:"error"`
	Data  []struct {
		Content string `json:"content"`
		ID      string `json:"id"`
	} `json:"data"`
}

func parseCertificates(spaceshipCommand *command.Model) ([]autoprovision.APICertificate, error) {
	output, err := runSpaceshipCommand(spaceshipCommand)
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

	var certInfos []autoprovision.APICertificate
	for _, certInfo := range certsResponse.Data {
		pemContent, err := base64.StdEncoding.DecodeString(certInfo.Content)
		if err != nil {
			return nil, err
		}

		cert, err := certificateutil.CeritifcateFromPemContent(pemContent)
		if err != nil {
			return nil, err
		}

		certInfos = append(certInfos, autoprovision.APICertificate{
			Certificate: certificateutil.NewCertificateInfo(*cert, nil),
			ID:          certInfo.ID,
		})
	}

	return certInfos, nil
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

func getSpaceshipCommand(targetDir, subCommand string, opts ...string) (*command.Model, error) {
	s := []string{"bundle", "exec", "ruby", "main.rb", "--subcommand", subCommand}
	s = append(s, opts...)
	spaceshipCmd, err := rubycommand.NewFromSlice(s)
	if err != nil {
		return nil, err
	}
	spaceshipCmd.SetDir(targetDir)

	return spaceshipCmd, nil
}

func runSpaceshipCommand(cmd *command.Model) (string, error) {
	fmt.Println()
	log.Donef("$ %s", cmd.PrintableCommandArgs())
	output, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		return "", fmt.Errorf("spaceship command failed: %s, error: %s", output, err)
	}

	jsonRegexp := regexp.MustCompile(`\n\{.*\}$`)
	match := jsonRegexp.FindString(output)
	if match == "" {
		return "", fmt.Errorf("output does not contain response: %s", output)
	}

	return match, nil
}

func createProfile() error {
	client, err := NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize Spaceship client: %v")
	}

	s := NewSpaceshipCertificateSource(client)

	certs, err := s.QueryAllIOSCertificates()
	if err != nil {
		return err
	}
	devCerts := certs[appstoreconnect.IOSDevelopment]
	cert := devCerts[0]

	spaceshipDir, err := getSpaceshipDirectory()
	if err != nil {
		return err
	}

	cmd, err := getSpaceshipCommand(spaceshipDir, "create_profile",
		"--bundle_id", "io.bitrise.ios.Fennec",
		"--certificate", cert.ID,
		"--profile_name", "lib_test",
	)
	if err != nil {
		return err
	}

	output, err := runSpaceshipCommand(cmd)
	fmt.Println(output)
	return err
}
