package autoprovision

import (
	"fmt"
	"strings"
)

func createWildcardBundleID(bundleID string) (string, error) {
	idx := strings.LastIndex(bundleID, ".")
	if idx == -1 {
		return "", fmt.Errorf("invalid bundle id (%s): does not contain *", bundleID)
	}

	return bundleID[:idx] + ".*", nil
}
