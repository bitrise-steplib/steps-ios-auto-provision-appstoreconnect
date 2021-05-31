#!/bin/env bash
set -e

echo "Inputs:"
echo "project: $project"
echo "target: $target"
echo "configuration: $configuration"
echo "development_team: $development_team"
echo "code_sign_identity: $code_sign_identity"
echo "provisioning_profile: $provisioning_profile"

out=$(xcodebuild -showBuildSettings -configuration $configuration -target $target -project $project)
if [[ $out != *"CODE_SIGN_STYLE = Manual"* ]] ; then
    echo "invalid CODE_SIGN_STYLE for sample-apps-fastlane-testUITests target"
    exit 1
fi
if [[ $out != *"DEVELOPMENT_TEAM = $development_team"* ]] ; then
    echo "invalid DEVELOPMENT_TEAM for sample-apps-fastlane-testUITests target"
    exit 1
fi
if [[ $out != *"CODE_SIGN_IDENTITY = $code_sign_identity"* ]] ; then
    echo "invalid CODE_SIGN_IDENTITY for sample-apps-fastlane-testUITests target"
    exit 1
fi
if [[ $out == *"PROVISIONING_PROFILE_SPECIFIER"* ]] ; then
    echo "invalid PROVISIONING_PROFILE_SPECIFIER for sample-apps-fastlane-testUITests target"
    exit 1
fi
if [[ $out != *"PROVISIONING_PROFILE = $provisioning_profile"* ]] ; then
    echo "invalid PROVISIONING_PROFILE for sample-apps-fastlane-testUITests target"
    exit 1
fi