package org

import (
	"testing"


	sdk "github.com/openshift-online/ocm-sdk-go"
	
	"bou.ke/monkey"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestDescribeOrg(t *testing.T) {
	cmd := &cobra.Command{}
	orgID := "o-1234567890"
	monkey.Patch(sendDescribeOrgRequest, func(string) (*sdk.Response, error) {
		mockResp := &sdk.Response{}
		return mockResp, nil
	})
	err := describeOrg(cmd, orgID)

	if err != nil {
		t.Fatalf("describeOrg failed: %v", err)
	}
}

func TestCreateDescribeRequest(t *testing.T) {
	ocmClient := &sdk.Connection{}
	orgID := "test-org-id"
	request := createDescribeRequest(ocmClient, orgID)
	actualPath := request.GetPath()
	expectedPath := organizationsAPIPath + "/" + orgID

	assert.NotNil(t, request, "Request should not be nil")
	assert.Contains(t, actualPath, expectedPath, "Request path should contain the expected path")
}
