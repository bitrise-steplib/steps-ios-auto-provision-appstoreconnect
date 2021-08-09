# iOS Auto Provision with App Store Connect API

Automatically manages your iOS Provisioning Profiles for your Xcode project.

Links:

- [Changelog](https://github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/releases)

<details>
<summary>Description</summary>

The Step uses the official [App Store Connect API](https://developer.apple.com/documentation/appstoreconnectapi/generating_tokens_for_api_requests).
The Steps performs the following:
- It generates, updates and downloads the provisioning profiles needed for your iOS project.
- It verifies if your project is registered with the App Store Connect. If it was not, the Step registers your project.
- It registers the iOS devices connected to your Bitrise account with the App Store Connect.
- It modifies the iOS project to use manual code signing if the project uses automatically managed signing.

### Configuring the Step

Before you start configuring the Step, make sure you've completed the following requirements:
1. You've generated an [API key](https://developer.apple.com/documentation/appstoreconnectapi/creating_api_keys_for_app_store_connect_api) and obtained an **Issuer ID**, **Key ID** and a **Private Key**.
2. You've [defined your Apple Developer Account to Bitrise](https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/#defining-your-apple-developer-account-to-bitrise).
3. You've [assigned an Apple Developer Account to your app](https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/#assigning-an-apple-developer-account-for-your-app).

Once these are done, most of the required Step input fields are already populated for you.

To configure the Step:

1. Add the **iOS Auto Provision with App Store Connect API** after any dependency installer Step in your Workflow, such as **Run CocoaPods install** or **Carthage**.
2. Click the Step to edit its input fields. You can see that the **Build API token**, **Build URL**, and the **Xcode Project (or Workspace) path** inputs are automatically filled out for you.
   - **Build API token**: Every running build has a temporary API token on a Bitrise virtual machine. This token is only available while the build is running. The Step downloads the connected API key with the help of this API token and Bitrise saves it in a JSON file.
   - **Build URL**: URL of the current build or local path URL to your `apple_developer_portal_data.json`.
   - **Xcode Project path**: The path where the `.xcodeproj` / `.xcworkspace` is located.
3. **Distribution type** input's value has to match with the value of the **Select method for export** input in the **Xcode Archive & Export for iOS** Step.
4. With the **Scheme name** input you can restrict which targets to process.

### Troubleshooting
Make sure you do not have the **Certificate and Profile Installer** Step in your Workflow.
Make sure that you do NOT modify your Xcode project between the **iOS Auto Provision with App Store Connect API** and the **Xcode Archive & Export for iOS** Steps. For example, do not change the **bundle ID** after the **iOS Auto Provision with App Store Connect API** Step.

### Useful links
- [Managing iOS code signing files - automatic provisioning](https://devcenter.bitrise.io/code-signing/ios-code-signing/ios-auto-provisioning/)
- [About iOS Auto Provision with Apple ID](https://devcenter.bitrise.io/getting-started/configuring-bitrise-steps-that-require-apple-developer-account-data/#assigning-an-apple-developer-account-for-your-appv)

### Related Steps
- [iOS Auto Provision with Apple ID](https://www.bitrise.io/integrations/steps/ios-auto-provision)
- [Xcode Archive & Export](https://www.bitrise.io/integrations/steps/xcode-archive)
</details>

## 🧩 Get started

Add this step directly to your workflow in the [Bitrise Workflow Editor](https://devcenter.bitrise.io/steps-and-workflows/steps-and-workflows-index/).

You can also run this step directly with the [bitrise CLI](https://github.com/bitrise-io/bitrise)

## ⚙️ Configuration

<details>
<summary>Inputs</summary>

### Bitrise Apple Developer Connection*

Key: `connection` (default: `automatic`)

Values:

- `automatic`
- `api_key`
- `off`

The input determines the method used for Apple Service authentication. By default, the Bitrise Apple Developer connection based on API key is used and other authentication-related Step inputs are ignored.

You can either use the established Bitrise Apple Developer connection or you can tell the Step to only use the Step inputs for authentication.
- `automatic`: Use the Apple Developer connection based on API key. Step inputs are only used as a fallback.
- `api_key`: Use the Apple Developer connection based on API key authentication. Authentication-related Step inputs are ignored.
- `off`: Do not use any already configured Apple Developer Connection. Only authentication-related Step inputs are considered.

### API Key URL

Key: `api_key_path`


Specify the path in an URL format where your API key is stored.

For example: `https://URL/TO/AuthKey_[KEY_ID].p8` or `file:///PATH/TO/AuthKey_[KEY_ID].p8`.
**NOTE:** The Step will only recognize the API key if the filename includes the  `KEY_ID` value as shown on the examples above.

You can upload your key on the **Generic File Storage** tab in the Workflow Editor and set the Environment Variable for the file here.

For example: `$BITRISEIO_MYKEY_URL`

### API Key Issuer ID

Key: `api_issuer`


Issuer ID. Required if **API Key URL** (`api_key_path`) is specified.

### Distribution type*

Key: `distribution_type` (default: `development`)

Values:

- `development`
- `app-store`
- `ad-hoc`
- `enterprise`

Describes how Xcode should sign your project.

### Xcode Project (or Workspace) path*

Key: `project_path` (default: `$BITRISE_PROJECT_PATH`)


The path where the `.xcodeproj` / `.xcworkspace` is located.

### Scheme name*

Key: `scheme` (default: `$BITRISE_SCHEME`)


The scheme selects the main Application Target of the project.

The step will manage the codesign settings of the main Application and related executable (Application and App Extension) targets.

### Configuration name

Key: `configuration`


Configuration (for example, Debug, Release) selects the Build Settings describing the managed executable targets' Signing (Code Signing Style, Development Team, Code Signing Identity, Provisioning Profile).

If not set the step will use the provided Scheme's Archive Action's Build Configuration.

### Should the step manage UITest target's codesigning?

Key: `sign_uitest_targets` (default: `no`)

Values:

- `yes`
- `no`

If set the step will manage the codesign settings of the UITest targets of the main Application.
The UITest targets' bundle id will be set to the main Application's bundle id, so that the same Signing can be used for both the main Application and related UITest targets.

### Should the step register test devices with the Apple Developer Portal?

Key: `register_test_devices` (default: `no`)

Values:

- `yes`
- `no`

If set the step will register known test devices from team members with the Apple Developer Portal.

Note that setting this to "yes" may cause devices to be registered against your limited quantity of test devices in the Apple Developer Portal, which can only be removed once annually during your renewal window.

### The minimum days the Provisioning Profile should be valid

Key: `min_profile_days_valid`


Sometimes you want to sign an app with a Provisioning Profile that is valid for at least 'x' days.
For example, an enterprise app won't open if your Provisioning Profile is expired. With this parameter, you can have a Provisioning Profile that's at least valid for 'x' days.
By default it is set to `0` and renews the Provisioning Profile when expired.

### Enable verbose logging?*

Key: `verbose_log` (default: `no`)

Values:

- `yes`
- `no`

Enable verbose logging?

### Certificate URL*

Key: `certificate_urls` (default: `$BITRISE_CERTIFICATE_URL`)


URLs of the certificates to download.
Multiple URLs can be specified, separated by a pipe (`|`) character,
you can specify a local path as well, using the `file://` scheme.
__Provide a development certificate__ URL, to ensure development code signing files for the project and __also provide a distribution certificate__ URL, to ensure distribution code signing files for your project, for example, `file://./development/certificate/path|https://distribution/certificate/url`


### Certificate passphrase*

Key: `passphrases` (default: `$BITRISE_CERTIFICATE_PASSPHRASE`)


Certificate passphrases.
Multiple passphrases can be specified, separated by a pipe (`|`) character.
__Specified certificate passphrase count should match the count of the certificate urls__,for example, (1 certificate with empty passphrase, 1 certificate with non-empty passphrase): `|distribution-passphrase`


### Keychain path*

Key: `keychain_path` (default: `$HOME/Library/Keychains/login.keychain`)


The Keychain path.

### Keychain's password*

Key: `keychain_password` (default: `$BITRISE_KEYCHAIN_PASSWORD`)


The Keychain's password.

### Build API token

Key: `build_api_token` (default: `$BITRISE_BUILD_API_TOKEN`)


Every build gets a temporary Bitrise API token to download the connected API key in a JSON file.

### Build URL

Key: `build_url` (default: `$BITRISE_BUILD_URL`)


URL of the current build or local path URL to your apple_developer_portal_data.json.

</details>

<details>
<summary>Outputs</summary>

### `$BITRISE_EXPORT_METHOD`

The selected distribution type

Distribution type can be one of the following: `development`, `app-store`, `ad-hoc` or `enterprise`.

### `$BITRISE_DEVELOPER_TEAM`

The development team's ID

The development team's ID, for example, `1MZX23ABCD4`.

### `$BITRISE_DEVELOPMENT_CODESIGN_IDENTITY`

The development codesign identity's name

The development codesign identity's name, for example, `iPhone Developer: Bitrise Bot (VV2J4SV8V4)`.

### `$BITRISE_PRODUCTION_CODESIGN_IDENTITY`

The production codesign identity's name

The production codesign identity's name, for example, `iPhone Distribution: Bitrise Bot (VV2J4SV8V4.

### `$BITRISE_DEVELOPMENT_PROFILE`

The main target's development provisioning profile UUID

The development provisioning profile's UUID which belongs to the main target, for example, `c5be4123-1234-4f9d-9843-0d9be985a068`.

### `$BITRISE_PRODUCTION_PROFILE`

The main target's production provisioning profile UUID

The production provisioning profile's UUID which belongs to the main target, for example, `c5be4123-1234-4f9d-9843-0d9be985a068`.
</details>

## 🙋 How to contribute

Links:

- [Create your own step](https://devcenter.bitrise.io/contributors/create-your-own-step/)
- [Testing and versioning your Step](https://devcenter.bitrise.io/contributors/testing-and-versioning-your-steps/)