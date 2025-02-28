package org

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"unsafe"

	"bou.ke/monkey"
	sdk "github.com/openshift-online/ocm-sdk-go"
	accountsv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"

	"github.com/openshift/osdctl/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestCheckOrgId(t *testing.T) {
	tests := []struct {
		Name          string
		Args          []string
		ErrorExpected bool
	}{
		{
			Name:          "Org Id provided",
			Args:          []string{"testOrgId"},
			ErrorExpected: false,
		},
		{
			Name:          "No Org Id provided",
			Args:          []string{},
			ErrorExpected: true,
		},
		{
			Name:          "Multiple Org id provided",
			Args:          []string{"testOrgId1", "testOrgId2"},
			ErrorExpected: true,
		},
	}

	for _, test := range tests {
		err := checkOrgId(test.Args)
		if test.ErrorExpected {
			if err == nil {
				t.Fatalf("Test '%s' failed. Expected error, but got none", test.Name)
			}
		} else {
			if err != nil {
				t.Fatalf("Test '%s' failed. Expected no error, but got '%v'", test.Name, err)
			}
		}
	}
}

func TestPrintJson(t *testing.T) {
	// Test data
	testData := map[string]string{
		"name":  "test",
		"value": "123",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	defer r.Close()
	os.Stdout = w

	PrintJson(testData)

	err = w.Close()
	if err != nil {
		t.Errorf("Failed to close writer: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	if err != nil {
		t.Errorf("Failed to read from pipe: %v", err)
	}
	output := buf.String()

	expectedJSON, err := json.MarshalIndent(testData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal expected JSON: %v", err)
	}
	assert.Contains(t, output, string(expectedJSON), "Output should contain the expected JSON format")
}

func TestCreateGetSubscriptionsRequest(t *testing.T) {
	// Define test inputs
	fakeOrgID := "o-12345"
	fakeStatus := "Active"
	fakeManagedOnly := true
	fakePage := 1
	fakeSize := 20

	// Create a fake OCM client
	ocmClient := &sdk.Connection{}

	// Call the function under test
	request := createGetSubscriptionsRequest(ocmClient, fakeOrgID, fakeStatus, fakeManagedOnly, fakePage, fakeSize)

	// Assertions
	assert.NotNil(t, request, "Request should not be nil")
	assert.Equal(t, fakePage, getFieldValue(request, "page"), "Page number should be set correctly")
	assert.Equal(t, fakeSize, getFieldValue(request, "size"), "Size should be set correctly")

	// Verify search query format
	expectedQuery := fmt.Sprintf(`organization_id='%s' and status='%s' and managed=%v`, fakeOrgID, fakeStatus, fakeManagedOnly)
	assert.Equal(t, expectedQuery, getFieldValue(request, "search"), "Search query should match expected format")
}

// getFieldValue retrieves the value of a struct field using reflection
func getFieldValue(v interface{}, fieldName string) interface{} {
	rv := reflect.ValueOf(v).Elem()
	field := rv.FieldByName(fieldName)
	if !field.IsValid() {
		return fmt.Sprintf("Field '%s' not found", fieldName)
	}
	if !field.CanInterface() {
		// Handle unexported fields
		field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	}
	if field.Kind() == reflect.Ptr && !field.IsNil() {
		return field.Elem().Interface()
	}
	return field.Interface()
}

func TestGetSubscriptions(t *testing.T) {
	fakeOrgID := "o-12345"
	fakeStatus := "Active"
	fakeManagedOnly := true
	fakePage := 1
	fakeSize := 20

	monkey.Patch(utils.CreateConnection, func() (*sdk.Connection, error) {
		return &sdk.Connection{}, nil
	})

	monkey.Patch((*sdk.Connection).Close, func(*sdk.Connection) error {
		return nil
	})

	monkey.Patch((*accountsv1.SubscriptionsListRequest).Send, func(*accountsv1.SubscriptionsListRequest) (*accountsv1.SubscriptionsListResponse, error) {
		mockResponse := &accountsv1.SubscriptionsListResponse{}
		mockResponse.Status()
		return mockResponse, nil
	})

	response, err := getSubscriptions(fakeOrgID, fakeStatus, fakeManagedOnly, fakePage, fakeSize)
	fmt.Println(response, err)
	assert.NoError(t, err, "Function should not return an error")
	assert.NotNil(t, response, "Response should not be nil")
	monkey.UnpatchAll()
}

func createMockResponses(status string) *sdk.Response {
	response := &sdk.Response{}
	// Use reflection or public methods to set fields if available
	response.Status() // Assuming SetStatus is a public method
	return response
}

func TestSendRequest_Successs(t *testing.T) {
	// Create a properly initialized sdk.Request object
	request := &sdk.Request{}

	// Patch the Send method to return a mock response
	defer monkey.UnpatchAll() // Ensure the patch is removed after the test
	monkey.PatchInstanceMethod(
		reflect.TypeOf(request),
		"Send",
		func(*sdk.Request) (*sdk.Response, error) {
			return createMockResponses("OK"), nil
		},
	)

	// Call the function under test
	response, err := sendRequest(request)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, "OK", response.Status()) // Assuming GetStatus is a public method
}