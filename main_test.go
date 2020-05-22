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

	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/xcode-project/serialized"
	"github.com/bitrise-steplib/steps-ios-auto-provision-appstoreconnect/appstoreconnect"
	"github.com/stretchr/testify/mock"
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
		if !c.postProfileSuccess {
			c.postProfileSuccess = true
			return c.PostProfilesFailed(req)
		}
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
				"errors": []interface{}{map[string]interface{}{"detail": "multiple profiles found with the name"}},
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
		bundleIDByBundleIDIdentifer: map[string]*appstoreconnect.BundleID{"io.bitrise.testapp": &appstoreconnect.BundleID{
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
