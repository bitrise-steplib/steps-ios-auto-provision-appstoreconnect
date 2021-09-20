package autoprovision

import (
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
)

// ProfileManager ...
type ProfileManager struct {
	client                      ProfileClient
	bundleIDByBundleIDIdentifer map[string]*appstoreconnect.BundleID
	containersByBundleID        map[string][]string
}

// EnsureBundleID ...
func (m ProfileManager) EnsureBundleID(bundleIDIdentifier string, entitlements serialized.Object) (*appstoreconnect.BundleID, error) {
	fmt.Println()
	log.Infof("  Searching for app ID for bundle ID: %s", bundleIDIdentifier)

	bundleID, ok := m.bundleIDByBundleIDIdentifer[bundleIDIdentifier]
	if !ok {
		var err error
		bundleID, err = m.client.FindBundleID(bundleIDIdentifier)
		if err != nil {
			return nil, fmt.Errorf("failed to find bundle ID: %s", err)
		}
	}

	if bundleID != nil {
		log.Printf("  app ID found: %s", bundleID.Attributes.Name)

		m.bundleIDByBundleIDIdentifer[bundleIDIdentifier] = bundleID

		// Check if BundleID is sync with the project
		err := m.client.CheckBundleIDEntitlements(*bundleID, Entitlement(entitlements))
		if err != nil {
			if mErr, ok := err.(NonmatchingProfileError); ok {
				log.Warnf("  app ID capabilities invalid: %s", mErr.Reason)
				log.Warnf("  app ID capabilities are not in sync with the project capabilities, synchronizing...")
				if err := m.client.SyncBundleID(*bundleID, Entitlement(entitlements)); err != nil {
					return nil, fmt.Errorf("failed to update bundle ID capabilities: %s", err)
				}

				return bundleID, nil
			}

			return nil, fmt.Errorf("failed to validate bundle ID: %s", err)
		}

		log.Printf("  app ID capabilities are in sync with the project capabilities")

		return bundleID, nil
	}

	// Create BundleID
	log.Warnf("  app ID not found, generating...")

	capabilities := Entitlement(entitlements)

	bundleID, err := m.client.CreateBundleID(bundleIDIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to create bundle ID: %s", err)
	}

	containers, err := capabilities.ICloudContainers()
	if err != nil {
		return nil, fmt.Errorf("failed to get list of iCloud containers: %s", err)
	}

	if len(containers) > 0 {
		m.containersByBundleID[bundleIDIdentifier] = containers
		log.Errorf("  app ID created but couldn't add iCloud containers: %v", containers)
	}

	if err := m.client.SyncBundleID(*bundleID, capabilities); err != nil {
		return nil, fmt.Errorf("failed to update bundle ID capabilities: %s", err)
	}

	m.bundleIDByBundleIDIdentifer[bundleIDIdentifier] = bundleID

	return bundleID, nil
}

// EnsureProfile ...
func (m ProfileManager) EnsureProfile(profileType appstoreconnect.ProfileType, bundleIDIdentifier string, entitlements serialized.Object, certIDs, deviceIDs []string, minProfileDaysValid int) (*Profile, error) {
	fmt.Println()
	log.Infof("  Checking bundle id: %s", bundleIDIdentifier)
	log.Printf("  capabilities: %s", entitlements)

	// Search for Bitrise managed Profile
	name, err := ProfileName(profileType, bundleIDIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile name: %s", err)
	}

	profile, err := m.client.FindProfile(name, profileType)
	if err != nil {
		return nil, fmt.Errorf("failed to find profile: %s", err)
	}

	if profile == nil {
		log.Warnf("  profile does not exist, generating...")
	} else {
		log.Printf("  Bitrise managed profile found: %s ID: %s UUID: %s Expiry: %s", profile.Attributes().Name, profile.ID(), profile.Attributes().UUID, time.Time(profile.Attributes().ExpirationDate))

		if profile.Attributes().ProfileState == appstoreconnect.Active {
			// Check if Bitrise managed Profile is sync with the project
			err := CheckProfile(m.client, profile, Entitlement(entitlements), deviceIDs, certIDs, minProfileDaysValid)
			if err != nil {
				if mErr, ok := err.(NonmatchingProfileError); ok {
					log.Warnf("  the profile is not in sync with the project requirements (%s), regenerating ...", mErr.Reason)
				} else {
					return nil, fmt.Errorf("failed to check if profile is valid: %s", err)
				}
			} else { // Profile matches
				log.Donef("  profile is in sync with the project requirements")
				return &profile, nil
			}
		}

		if profile.Attributes().ProfileState == appstoreconnect.Invalid {
			// If the profile's bundle id gets modified, the profile turns in Invalid state.
			log.Warnf("  the profile state is invalid, regenerating ...")
		}

		if err := m.client.DeleteProfile(profile.ID()); err != nil {
			return nil, fmt.Errorf("failed to delete profile: %s", err)
		}
	}

	// Search for BundleID
	bundleID, err := m.EnsureBundleID(bundleIDIdentifier, entitlements)
	if err != nil {
		return nil, err
	}

	// Create Bitrise managed Profile
	fmt.Println()
	log.Infof("  Creating profile for bundle id: %s", bundleID.Attributes.Name)

	profile, err = m.client.CreateProfile(name, profileType, *bundleID, certIDs, deviceIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile: %s", err)
	}

	log.Donef("  profile created: %s", profile.Attributes().Name)
	return &profile, nil
}
