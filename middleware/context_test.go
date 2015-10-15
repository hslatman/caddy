package middleware

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInclude(t *testing.T) {
	context := getContextOrFail(t)

	inputFilename := "test_file"
	absInFilePath := filepath.Join(fmt.Sprintf("%s", context.Root), inputFilename)
	defer func() {
		err := os.Remove(absInFilePath)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("Failed to clean test file!")
		}
	}()

	tests := []struct {
		fileContent          string
		expectedContent      string
		shouldErr            bool
		expectedErrorContent string
	}{
		// Test 0 - all good
		{
			fileContent:          `str1 {{ .Root }} str2`,
			expectedContent:      fmt.Sprintf("str1 %s str2", context.Root),
			shouldErr:            false,
			expectedErrorContent: "",
		},
		// Test 1 - failure on template.Parse
		{
			fileContent:          `str1 {{ .Root } str2`,
			expectedContent:      "",
			shouldErr:            true,
			expectedErrorContent: `unexpected "}" in operand`,
		},
		// Test 3 - failure on template.Execute
		{
			fileContent:          `str1 {{ .InvalidField }} str2`,
			expectedContent:      "",
			shouldErr:            true,
			expectedErrorContent: `InvalidField is not a field of struct type middleware.Context`,
		},
	}

	for i, test := range tests {
		testPrefix := getTestPrefix(i)

		// WriteFile truncates the contentt
		err := ioutil.WriteFile(absInFilePath, []byte(test.fileContent), os.ModePerm)
		if err != nil {
			t.Fatal(testPrefix+"Failed to create test file. Error was: %v", err)
		}

		content, err := context.Include(inputFilename)
		if err != nil {
			if !test.shouldErr {
				t.Errorf(testPrefix+"Expected no error, found [%s]", test.expectedErrorContent, err.Error())
			}
			if !strings.Contains(err.Error(), test.expectedErrorContent) {
				t.Errorf(testPrefix+"Expected error content [%s], found [%s]", test.expectedErrorContent, err.Error())
			}
		}

		if err == nil && test.shouldErr {
			t.Errorf(testPrefix+"Expected error [%s] but found nil. Input file was: %s", test.expectedErrorContent, inputFilename)
		}

		if content != test.expectedContent {
			t.Errorf(testPrefix+"Expected content [%s] but found [%s]. Input file was: %s", test.expectedContent, content, inputFilename)
		}
	}
}

func TestIncludeNotExisting(t *testing.T) {
	context := getContextOrFail(t)

	_, err := context.Include("not_existing")
	if err == nil {
		t.Errorf("Expected error but found nil!")
	}
}

func TestCookie(t *testing.T) {

	tests := []struct {
		cookie        *http.Cookie
		cookieName    string
		expectedValue string
	}{
		// Test 0 - happy path
		{
			cookie:        &http.Cookie{Name: "cookieName", Value: "cookieValue"},
			cookieName:    "cookieName",
			expectedValue: "cookieValue",
		},
		// Test 1 - try to get a non-existing cookie
		{
			cookie:        &http.Cookie{Name: "cookieName", Value: "cookieValue"},
			cookieName:    "notExisting",
			expectedValue: "",
		},
		// Test 2 - partial name match
		{
			cookie:        &http.Cookie{Name: "cookie", Value: "cookieValue"},
			cookieName:    "cook",
			expectedValue: "",
		},
		// Test 3 - cookie with optional fields
		{
			cookie:        &http.Cookie{Name: "cookie", Value: "cookieValue", Path: "/path", Domain: "https://caddy.com", Expires: (time.Now().Add(10 * time.Minute)), MaxAge: 120},
			cookieName:    "cookie",
			expectedValue: "cookieValue",
		},
	}

	for i, test := range tests {
		testPrefix := getTestPrefix(i)

		// reinitialize the context for each test
		context := getContextOrFail(t)

		context.Req.AddCookie(test.cookie)

		actualCookieVal := context.Cookie(test.cookieName)

		if actualCookieVal != test.expectedValue {
			t.Errorf(testPrefix+"Expected cookie value [%s] but found [%s] for cookie with name %s", test.expectedValue, actualCookieVal, test.cookieName)
		}
	}
}

func TestCookieMultipleCookies(t *testing.T) {
	context := getContextOrFail(t)

	cookieNameBase, cookieValueBase := "cookieName", "cookieValue"

	// make sure that there's no state and multiple requests for different cookies return the correct result
	for i := 0; i < 10; i++ {
		context.Req.AddCookie(&http.Cookie{Name: fmt.Sprintf("%s%d", cookieNameBase, i), Value: fmt.Sprintf("%s%d", cookieValueBase, i)})
	}

	for i := 0; i < 10; i++ {
		expectedCookieVal := fmt.Sprintf("%s%d", cookieValueBase, i)
		actualCookieVal := context.Cookie(fmt.Sprintf("%s%d", cookieNameBase, i))
		if actualCookieVal != expectedCookieVal {
			t.Fatalf("Expected cookie value %s, found %s", expectedCookieVal, actualCookieVal)
		}
	}
}

func TestHeader(t *testing.T) {
	context := getContextOrFail(t)

	headerKey, headerVal := "Header1", "HeaderVal1"
	context.Req.Header.Add(headerKey, headerVal)

	actualHeaderVal := context.Header(headerKey)
	if actualHeaderVal != headerVal {
		t.Errorf("Expected header %s, found %s", headerVal, actualHeaderVal)
	}

	missingHeaderVal := context.Header("not-existing")
	if missingHeaderVal != "" {
		t.Errorf("Expected empty header value, found %s", missingHeaderVal)
	}
}

func TestIP(t *testing.T) {
	context := getContextOrFail(t)

	tests := []struct {
		inputRemoteAddr string
		expectedIP      string
	}{
		// Test 0 - ipv4 with port
		{"1.1.1.1:1111", "1.1.1.1"},
		// Test 1 - ipv4 without port
		{"1.1.1.1", "1.1.1.1"},
		// Test 2 - ipv6 with port
		{"[::1]:11", "::1"},
		// Test 3 - ipv6 without port and brackets
		{"[2001:db8:a0b:12f0::1]", "[2001:db8:a0b:12f0::1]"},
		// Test 4 - ipv6 with zone and port
		{`[fe80:1::3%eth0]:44`, `fe80:1::3%eth0`},
		// Test 5 - ipv6 without port with brackets
		// {"[:fe:2]", ":fe:2"}, // TODO - failing (error in SplitHostPort) returns the host with brackets
		// Test 6 - invalid address
		// {":::::::::::::", ""}, // TODO - failing (error in SplitHostPort) returns the invalid address
		// Test 7 - invalid address
		// {"[::1][]", ""}, // TODO - failing (error in SplitHostPort) returns the invalid address
	}

	for i, test := range tests {
		testPrefix := getTestPrefix(i)

		context.Req.RemoteAddr = test.inputRemoteAddr
		actualIP := context.IP()

		if actualIP != test.expectedIP {
			t.Errorf(testPrefix+"Expected IP %s, found %s", test.expectedIP, actualIP)
		}
	}
}

func TestURL(t *testing.T) {
	context := getContextOrFail(t)

	inputURL := "http://localhost"
	context.Req.RequestURI = inputURL

	if inputURL != context.URI() {
		t.Errorf("Expected url %s, found %s", inputURL, context.URI())
	}
}

func TestHost(t *testing.T) {
	tests := []struct {
		input        string
		expectedHost string
		shouldErr    bool
	}{
		{
			input:        "localhost:123",
			expectedHost: "localhost",
			shouldErr:    false,
		},
		{
			input:        "localhost",
			expectedHost: "",
			shouldErr:    true, // missing port in address
		},
	}

	for _, test := range tests {
		testHostOrPort(t, true, test.input, test.expectedHost, test.shouldErr)
	}
}

func TestPort(t *testing.T) {
	tests := []struct {
		input        string
		expectedPort string
		shouldErr    bool
	}{
		{
			input:        "localhost:123",
			expectedPort: "123",
			shouldErr:    false,
		},
		{
			input:        "localhost",
			expectedPort: "",
			shouldErr:    true, // missing port in address
		},
	}

	for _, test := range tests {
		testHostOrPort(t, false, test.input, test.expectedPort, test.shouldErr)
	}
}

func testHostOrPort(t *testing.T, isTestingHost bool, input, expectedResult string, shouldErr bool) {
	context := getContextOrFail(t)

	context.Req.Host = input
	var actualResult, testedObject string
	var err error

	if isTestingHost {
		actualResult, err = context.Host()
		testedObject = "host"
	} else {
		actualResult, err = context.Port()
		testedObject = "port"
	}

	if shouldErr && err == nil {
		t.Errorf("Expected error, found nil!")
		return
	}

	if !shouldErr && err != nil {
		t.Errorf("Expected no error, found %s", err)
		return
	}

	if actualResult != expectedResult {
		t.Errorf("Expected %s %s, found %s", testedObject, expectedResult, actualResult)
	}
}

func TestMethod(t *testing.T) {
	context := getContextOrFail(t)

	method := "POST"
	context.Req.Method = method

	if method != context.Method() {
		t.Errorf("Expected method %s, found %s", method, context.Method())
	}

}

func TestPathMatches(t *testing.T) {
	context := getContextOrFail(t)

	tests := []struct {
		urlStr      string
		pattern     string
		shouldMatch bool
	}{
		// Test 0
		{
			urlStr:      "http://caddy.com/",
			pattern:     "",
			shouldMatch: true,
		},
		// Test 1
		{
			urlStr:      "http://caddy.com",
			pattern:     "",
			shouldMatch: true,
		},
		// Test 1
		{
			urlStr:      "http://caddy.com/",
			pattern:     "/",
			shouldMatch: true,
		},
		// Test 3
		{
			urlStr:      "http://caddy.com/?param=val",
			pattern:     "/",
			shouldMatch: true,
		},
		// Test 4
		{
			urlStr:      "http://caddy.com/dir1/dir2",
			pattern:     "/dir2",
			shouldMatch: false,
		},
		// Test 5
		{
			urlStr:      "http://caddy.com/dir1/dir2",
			pattern:     "/dir1",
			shouldMatch: true,
		},
		// Test 6
		{
			urlStr:      "http://caddy.com:444/dir1/dir2",
			pattern:     "/dir1",
			shouldMatch: true,
		},
		// Test 7
		{
			urlStr:      "http://caddy.com/dir1/dir2",
			pattern:     "*/dir2",
			shouldMatch: false,
		},
	}

	for i, test := range tests {
		testPrefix := getTestPrefix(i)
		var err error
		context.Req.URL, err = url.Parse(test.urlStr)
		if err != nil {
			t.Fatalf("Failed to prepare test URL from string %s! Error was: %s", test.urlStr, err)
		}

		matches := context.PathMatches(test.pattern)
		if matches != test.shouldMatch {
			t.Errorf(testPrefix+"Expected and actual result differ: expected to match [%t], actual matches [%t]", test.shouldMatch, matches)
		}
	}
}

func initTestContext() (Context, error) {
	rootDir := getTestFilesFolder()
	body := bytes.NewBufferString("request body")
	request, err := http.NewRequest("GET", "https://caddy.com", body)
	if err != nil {
		return Context{}, err
	}
	return Context{Root: http.Dir(rootDir), Req: request}, nil
}

func getContextOrFail(t *testing.T) Context {
	context, err := initTestContext()
	if err != nil {
		t.Fatalf("Failed to prepare test context")
	}
	return context
}

func getTestFilesFolder() string {
	return os.TempDir()
}

func getTestPrefix(testN int) string {
	return fmt.Sprintf("Test [%d]: ", testN)
}
