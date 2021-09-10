package spaceship

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/autoprovision"
)

type SpaceshipProfileClient struct {
	client *Client
}

func NewSpaceshipProfileClient(client *Client) *SpaceshipProfileClient {
	return &SpaceshipProfileClient{client: client}
}

type ProfileInfo struct {
	ID           string                           `json:"id"`
	UUID         string                           `json:"uuid"`
	Name         string                           `json:"name"`
	Status       string                           `json:"status"` // "Active" "Expired" "Invalid"
	Expiry       time.Time                        `json:"expiry"`
	Platform     appstoreconnect.BundleIDPlatform `json:"platform"`
	Content      string                           `json:"content"`
	AppID        string                           `json:"app_id"`
	BundleID     string                           `json:"bundle_id"`
	Certificates []string                         `json:"certificates"`
	Devices      []string                         `json:"devices"`
}

type AppInfo struct {
	ID       string `json:"id"`
	BundleID string `json:"bundleID"`
}

func (c *SpaceshipProfileClient) FindProfile(name string, profileType appstoreconnect.ProfileType) (*appstoreconnect.Profile, error) {
	cmd, err := c.client.createRequestCommand("list_profiles", "--name", name, "--profile-type", string(profileType))
	if err != nil {
		return nil, err
	}

	output, err := runSpaceshipCommand(cmd)
	if err != nil {
		return nil, err
	}

	var profileResponse struct {
		Data []ProfileInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &profileResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	var match *ProfileInfo
	for _, p := range profileResponse.Data {
		if name == p.Name {
			match = &p
			break
		}
	}

	if match == nil {
		return nil, nil
	}

	profileContent, err := base64.StdEncoding.DecodeString(match.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode profile contents: %v", err)
	}

	return &appstoreconnect.Profile{
		ID: match.ID,
		Attributes: appstoreconnect.ProfileAttributes{
			Name:           match.Name,
			UUID:           match.UUID,
			ProfileContent: profileContent,
		},
	}, nil
}

func (c *SpaceshipProfileClient) DeleteExpiredProfile(bundleID *appstoreconnect.BundleID, profileName string) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) CheckProfile(prof appstoreconnect.Profile, entitlements autoprovision.Entitlement, deviceIDs, certificateIDs []string, minProfileDaysValid int) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) DeleteProfile(id string) error {
	log.Infof("DeleteProfile (%s)", id)
	cmd, err := c.client.createRequestCommand("delete_profile", "--id", id)
	if err != nil {
		return err
	}

	_, err = runSpaceshipCommand(cmd)
	if err != nil {
		return err
	}

	return nil
}

func (c *SpaceshipProfileClient) CreateProfile(name string, profileType appstoreconnect.ProfileType, bundleID appstoreconnect.BundleID, certificateIDs []string, deviceIDs []string) (*appstoreconnect.Profile, error) {
	cmd, err := c.client.createRequestCommand("create_profile",
		"--bundle_id", bundleID.Attributes.Identifier,
		"--certificate", certificateIDs[0],
		"--profile_name", name,
		"--profile-type", string(profileType),
	)
	if err != nil {
		return nil, err
	}

	output, err := runSpaceshipCommand(cmd)
	if err != nil {
		return nil, err
	}

	var profileResponse struct {
		Data *ProfileInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &profileResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v (%s)", err, output)
	}

	return &appstoreconnect.Profile{
		ID: profileResponse.Data.ID,
		Attributes: appstoreconnect.ProfileAttributes{
			UUID:     profileResponse.Data.UUID,
			Name:     profileResponse.Data.Name,
			Platform: profileResponse.Data.Platform,
		},
	}, nil
}

func (c *SpaceshipProfileClient) FindBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error) {
	cmd, err := c.client.createRequestCommand("get_app", "--bundle_id", bundleIDIdentifier)
	if err != nil {
		return nil, err
	}

	output, err := runSpaceshipCommand(cmd)
	if err != nil {
		return nil, err
	}

	var appResponse struct {
		Data AppInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &appResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	return &appstoreconnect.BundleID{
		ID: appResponse.Data.ID,
		Attributes: appstoreconnect.BundleIDAttributes{
			Identifier: appResponse.Data.BundleID,
		},
	}, nil
}

func (c *SpaceshipProfileClient) CheckBundleIDEntitlements(bundleID appstoreconnect.BundleID, projectEntitlements autoprovision.Entitlement) error {
	return nil
}

func (c *SpaceshipProfileClient) SyncBundleID(bundleIDID string, entitlements autoprovision.Entitlement) error {
	panic("implement")
}

func (c *SpaceshipProfileClient) CreateBundleID(bundleIDIdentifier string) (*appstoreconnect.BundleID, error) {
	panic("implement")
}
