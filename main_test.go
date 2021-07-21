package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockClient struct {
	mock.Mock
	postProfileSuccess bool
}

func (c *MockClient) Do(req *http.Request) (*http.Response, error) {
	fmt.Printf("do called: %#v - %#v\n", req.Method, req.URL.Path)

	switch {
	case req.URL.Path == "/v1/profiles" && req.Method == "GET":
		return c.GetProfiles(req)
	case req.URL.Path == "/v1/profiles" && req.Method == "POST":
		// First profile create request fails by 'Multiple profiles found' error
		if !c.postProfileSuccess {
			c.postProfileSuccess = true
			return c.PostProfilesFailed(req)
		}
		// After deleting the expired profile, creating a new one succeed
		return c.PostProfilesSuccess(req)
	case req.URL.Path == "/v1//bundleID/capabilities" && req.Method == "GET":
		return c.GetBundleIDCapabilities(req)
	case req.URL.Path == "/v1//bundleID/profiles" && req.Method == "GET":
		return c.GetBundleIDProfiles(req)
	case req.URL.Path == "/v1/profiles/1" && req.Method == "DELETE":
		return c.DeleteProfiles(req)
	}

	return nil, fmt.Errorf("invalid endpoint called: %s, method: %s", req.URL.Path, req.Method)
}

func (c *MockClient) GetProfiles(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func (c *MockClient) PostProfilesFailed(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func (c *MockClient) GetBundleIDCapabilities(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func (c *MockClient) GetBundleIDProfiles(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func (c *MockClient) DeleteProfiles(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func (c *MockClient) PostProfilesSuccess(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func newResponse(t *testing.T, status int, body map[string]interface{}) *http.Response {
	resp := http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       ioutil.NopCloser(nil),
	}

	if body != nil {
		var buff bytes.Buffer
		require.NoError(t, json.NewEncoder(&buff).Encode(body))
		resp.Body = ioutil.NopCloser(&buff)
		resp.ContentLength = int64(buff.Len())
	}

	return &resp
}

func TestEnsureProfile_ExpiredProfile(t *testing.T) {
	// Arrange
	mockClient := &MockClient{}

	mockClient.
		On("GetProfiles", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusOK, map[string]interface{}{}), nil)

	mockClient.
		On("PostProfilesFailed", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusConflict,
			map[string]interface{}{
				"errors": []interface{}{map[string]interface{}{"detail": "ENTITY_ERROR: There is a problem with the request entity: Multiple profiles found with the name 'Bitrise iOS development - (io.bitrise.testapp)'.  Please remove the duplicate profiles and try again."}},
			}), nil)

	mockClient.
		On("GetBundleIDCapabilities", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusOK, map[string]interface{}{}), nil)

	mockClient.
		On("GetBundleIDProfiles", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusOK,
			map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{
						"attributes": map[string]interface{}{"name": "Bitrise iOS development - (io.bitrise.testapp)"},
						"id":         "1",
					},
				}},
		), nil)

	mockClient.
		On("DeleteProfiles", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusOK, map[string]interface{}{}), nil)

	mockClient.
		On("PostProfilesSuccess", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusOK, map[string]interface{}{}), nil)

	client := appstoreconnect.NewClient(mockClient, "keyID", "issueID", []byte("privateKey"))
	manager := ProfileManager{
		client: client,
		// cache io.bitrise.testapp bundle ID, so that no need to mock bundle ID GET requests
		bundleIDByBundleIDIdentifer: map[string]*appstoreconnect.BundleID{"io.bitrise.testapp": {
			Relationships: appstoreconnect.BundleIDRelationships{
				Profiles: appstoreconnect.RelationshipsLinks{
					Links: appstoreconnect.Links{
						Related: "https://api.appstoreconnect.apple.com/v1/bundleID/profiles",
					},
				},
				Capabilities: appstoreconnect.RelationshipsLinks{
					Links: appstoreconnect.Links{
						Related: "https://api.appstoreconnect.apple.com/v1/bundleID/capabilities",
					},
				},
			},
		}},
		containersByBundleID: nil}

	// Act
	profile, err := manager.EnsureProfile(
		appstoreconnect.IOSAppDevelopment,
		"io.bitrise.testapp",
		serialized.Object(map[string]interface{}{}),
		[]string{},
		[]string{},
		0,
	)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, profile)
	mockClient.AssertExpectations(t)
}

func TestDownloadLocalCertificates(t *testing.T) {
	const teamID = "MYTEAMID"
	const commonName = "Apple Developer: test"
	const teamName = "BITFALL FEJLESZTO KORLATOLT FELELOSSEGU TARSASAG"
	expiry := time.Now().AddDate(1, 0, 0)
	serial := int64(1234)

	cert, privateKey, err := certificateutil.GenerateTestCertificate(serial, teamID, teamName, commonName, expiry)
	if err != nil {
		t.Errorf("init: failed to generate certificate: %s", err)
	}

	certInfo := certificateutil.NewCertificateInfo(*cert, privateKey)
	t.Logf("Test certificate generated. Serial: %s Team ID: %s Common name: %s", certInfo.Serial, certInfo.TeamID, certInfo.CommonName)

	passphrase := ""
	certData, err := certInfo.EncodeToP12(passphrase)
	if err != nil {
		t.Errorf("init: failed to encode certificate: %s", err)
	}

	p12File, err := ioutil.TempFile("", "*.p12")
	if err != nil {
		t.Errorf("init: failed to create temp test file: %s", err)
	}

	if _, err = p12File.Write(certData); err != nil {
		t.Errorf("init: failed to write test file: %s", err)
	}

	if err = p12File.Close(); err != nil {
		t.Errorf("init: failed to close file: %s", err)
	}

	p12path := "file://" + p12File.Name()

	tests := []struct {
		name    string
		URLs    []CertificateFileURL
		want    []certificateutil.CertificateInfoModel
		wantErr bool
	}{
		{
			name: "Certificate matches generated.",
			URLs: []CertificateFileURL{{
				URL:        p12path,
				Passphrase: passphrase,
			}},
			want: []certificateutil.CertificateInfoModel{
				certInfo,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := downloadCertificates(tt.URLs)
			if (err != nil) != tt.wantErr {
				t.Errorf("DownloadLocalCertificates() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DownloadLocalCertificates() = %v, want %v", got, tt.want)
			}
		})
	}
}

type MockClientRegisterDevice struct {
	mock.Mock
}

func (c *MockClientRegisterDevice) Do(req *http.Request) (*http.Response, error) {
	fmt.Printf("do called: %#v - %#v\n", req.Method, req.URL.Path)

	switch {
	case req.URL.Path == "/v1/devices" && req.Method == "POST":
		return c.PostDevice(req)
	}

	return nil, fmt.Errorf("invalid endpoint called: %s, method: %s", req.URL.Path, req.Method)
}

func (c *MockClientRegisterDevice) PostDevice(req *http.Request) (*http.Response, error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func Test_registerMissingDevices_alreadyRegistered(t *testing.T) {
	mockClient := &MockClientRegisterDevice{}
	successClient := appstoreconnect.NewClient(mockClient, "keyID", "issueID", []byte("privateKey"))

	args := struct {
		client           *appstoreconnect.Client
		bitriseDevices   []devportalservice.TestDevice
		devportalDevices []appstoreconnect.Device
	}{
		client: successClient,
		bitriseDevices: []devportalservice.TestDevice{{
			DeviceID:   "71153a920968f2842d360",
			DeviceType: "ios",
		}},
		devportalDevices: []appstoreconnect.Device{{
			Attributes: appstoreconnect.DeviceAttributes{
				UDID: "71153a920968f2842d360",
			},
			ID: "12",
		}},
	}
	want := []appstoreconnect.Device{}

	got, err := registerMissingDevices(args.client, args.bitriseDevices, args.devportalDevices, "")

	require.NoError(t, err, "registerMissingDevices() error")
	require.Equal(t, want, got, "registerMissingDevices()")
	mockClient.AssertNotCalled(t, "PostDevice")
	mockClient.AssertExpectations(t)
}

func Test_registerMissingDevices_newDevice(t *testing.T) {
	mockClient := &MockClientRegisterDevice{}
	mockClient.
		On("PostDevice", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusOK,
			map[string]interface{}{
				"data": map[string]interface{}{
					"id": "12",
				},
			},
		), nil)
	successClient := appstoreconnect.NewClient(mockClient, "keyID", "issueID", []byte("privateKey"))

	args := struct {
		client           *appstoreconnect.Client
		bitriseDevices   []devportalservice.TestDevice
		devportalDevices []appstoreconnect.Device
	}{
		client: successClient,
		bitriseDevices: []devportalservice.TestDevice{{
			DeviceID:   "71153a920968f2842d360",
			DeviceType: "ios",
		}},
		devportalDevices: []appstoreconnect.Device{},
	}
	want := []appstoreconnect.Device{{
		Attributes: appstoreconnect.DeviceAttributes{},
		ID:         "12",
	}}

	got, err := registerMissingDevices(args.client, args.bitriseDevices, args.devportalDevices, "")

	require.NoError(t, err, "registerMissingDevices()")
	require.Equal(t, want, got, "registerMissingDevices()")
	mockClient.AssertExpectations(t)
}

func Test_registerMissingDevices_invalidUDID(t *testing.T) {
	mockClient := &MockClientRegisterDevice{}
	mockClient.
		On("PostDevice", mock.AnythingOfType("*http.Request")).
		Return(newResponse(t, http.StatusConflict,
			map[string]interface{}{
				"errors": []interface{}{map[string]interface{}{"detail": "ENTITY_ERROR.ATTRIBUTE.INVALID: An attribute in the provided entity has invalid value: An invalid value 'xxx' was provided for the parameter 'udid'."}},
			}), nil)
	failureClient := appstoreconnect.NewClient(mockClient, "keyID", "issueID", []byte("privateKey"))

	args := struct {
		client           *appstoreconnect.Client
		bitriseDevices   []devportalservice.TestDevice
		devportalDevices []appstoreconnect.Device
	}{
		client: failureClient,
		bitriseDevices: []devportalservice.TestDevice{
			{
				DeviceID:   "invalid-udid",
				DeviceType: "ios",
			},
			{
				DeviceID:   "71153a920968f2842d360",
				DeviceType: "ios",
			},
		},
		devportalDevices: []appstoreconnect.Device{{
			Attributes: appstoreconnect.DeviceAttributes{
				UDID: "71153a920968f2842d360",
			},
			ID: "12",
		}},
	}
	want := []appstoreconnect.Device{}

	got, err := registerMissingDevices(args.client, args.bitriseDevices, args.devportalDevices, "")
	require.NoError(t, err, "registerMissingDevices()")
	require.Equal(t, want, got, "registerMissingDevices()")
	mockClient.AssertExpectations(t)
}

func Test_createWildcardBundleID(t *testing.T) {
	tests := []struct {
		name    string
		bundleID string
		want    string
		wantErr bool
	}{
		{
			name: "Invalid bundle id: empty",
			bundleID: "",
			want: "",
			wantErr: true,
		},
		{
			name: "Invalid bundle id: does not contain *",
			bundleID: "my_app",
			want: "",
			wantErr: true,
		},
		{
			name: "2 component bundle id",
			bundleID: "com.my_app",
			want: "com.*",
			wantErr: false,
		},
		{
			name: "multi component bundle id",
			bundleID: "com.bitrise.my_app.uitest",
			want: "com.bitrise.my_app.*",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createWildcardBundleID(tt.bundleID)
			if (err != nil) != tt.wantErr {
				t.Errorf("createWildcardBundleID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("createWildcardBundleID() got = %v, want %v", got, tt.want)
			}
		})
	}
}
