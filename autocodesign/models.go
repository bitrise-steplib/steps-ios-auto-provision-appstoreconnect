package autocodesign

import "github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"

// DistributionType ...
type DistributionType string

// DistributionTypes ...
var (
	Development DistributionType = "development"
	AppStore    DistributionType = "app-store"
	AdHoc       DistributionType = "ad-hoc"
	Enterprise  DistributionType = "enterprise"
)

// CertificateTypeByDistribution ...
var CertificateTypeByDistribution = map[DistributionType]appstoreconnect.CertificateType{
	Development: appstoreconnect.IOSDevelopment,
	AppStore:    appstoreconnect.IOSDistribution,
	AdHoc:       appstoreconnect.IOSDistribution,
	Enterprise:  appstoreconnect.IOSDistribution,
}

// Platform ...
type Platform string

// Const
const (
	IOS   Platform = "iOS"
	TVOS  Platform = "tvOS"
	MacOS Platform = "macOS"
)

// ProfileTypeToPlatform ...
var ProfileTypeToPlatform = map[appstoreconnect.ProfileType]Platform{
	appstoreconnect.IOSAppDevelopment: IOS,
	appstoreconnect.IOSAppStore:       IOS,
	appstoreconnect.IOSAppAdHoc:       IOS,
	appstoreconnect.IOSAppInHouse:     IOS,

	appstoreconnect.TvOSAppDevelopment: TVOS,
	appstoreconnect.TvOSAppStore:       TVOS,
	appstoreconnect.TvOSAppAdHoc:       TVOS,
	appstoreconnect.TvOSAppInHouse:     TVOS,
}

// ProfileTypeToDistribution ...
var ProfileTypeToDistribution = map[appstoreconnect.ProfileType]DistributionType{
	appstoreconnect.IOSAppDevelopment: Development,
	appstoreconnect.IOSAppStore:       AppStore,
	appstoreconnect.IOSAppAdHoc:       AdHoc,
	appstoreconnect.IOSAppInHouse:     Enterprise,

	appstoreconnect.TvOSAppDevelopment: Development,
	appstoreconnect.TvOSAppStore:       AppStore,
	appstoreconnect.TvOSAppAdHoc:       AdHoc,
	appstoreconnect.TvOSAppInHouse:     Enterprise,
}

// PlatformToProfileTypeByDistribution ...
var PlatformToProfileTypeByDistribution = map[Platform]map[DistributionType]appstoreconnect.ProfileType{
	IOS: {
		Development: appstoreconnect.IOSAppDevelopment,
		AppStore:    appstoreconnect.IOSAppStore,
		AdHoc:       appstoreconnect.IOSAppAdHoc,
		Enterprise:  appstoreconnect.IOSAppInHouse,
	},
	TVOS: {
		Development: appstoreconnect.TvOSAppDevelopment,
		AppStore:    appstoreconnect.TvOSAppStore,
		AdHoc:       appstoreconnect.TvOSAppAdHoc,
		Enterprise:  appstoreconnect.TvOSAppInHouse,
	},
}