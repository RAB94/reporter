/*
   Copyright 2016 Vastech SA (PTY) LTD

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package grafana

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is a Grafana API client
type Client interface {
	GetDashboard(dashName string) (Dashboard, error)
	GetPanelPng(p Panel, dashName string, t TimeRange) (io.ReadCloser, error)
	UsesGridLayout() bool
	// GetRowPng removed - no longer used
}

type client struct {
	url              string
	getDashEndpoint  func(dashName string) string
	getPanelEndpoint func(dashName string, vals url.Values) string // Used for panel rendering
	apiToken         string
	variables        url.Values
	sslCheck         bool
	useGridLayout    bool
}

// Retry configuration
var getPanelRetrySleepTime = time.Duration(2 * time.Second) // Base sleep time
const maxGetPanelRetries = 3
const renderRequestTimeout = 180 * time.Second // Keep increased timeout for panels

// NewV4Client (Keep as is, no GetRowPng to worry about)
func NewV4Client(baseURL string, apiToken string, variables url.Values, sslCheck bool, gridLayout bool) Client {
	log.Println("Using Grafana v4 client.")
	// ... (rest of V4 implementation remains the same) ...
	return &client{
		url: baseURL,
		getDashEndpoint: func(dashName string) string {
			return baseURL + "/api/dashboards/db/" + dashName
		},
		getPanelEndpoint: func(dashName string, vals url.Values) string {
			renderURL := baseURL + "/render/dashboard-solo/db/" + dashName + "?" + vals.Encode()
			return renderURL
		},
		apiToken:      apiToken,
		variables:     variables,
		sslCheck:      sslCheck,
		useGridLayout: gridLayout,
	}
}

// NewV5Client (Keep as is, no GetRowPng to worry about)
func NewV5Client(baseURL string, apiToken string, variables url.Values, sslCheck bool, gridLayout bool) Client {
	log.Println("Using Grafana v5 client.")
	// ... (rest of V5 implementation remains the same) ...
	return &client{
		url: baseURL,
		getDashEndpoint: func(dashName string) string {
			isUID := false
			if len(dashName) > 8 { // Basic UID heuristic
				isUID = true
				for _, r := range dashName {
					if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
						isUID = false
						break
					}
				}
			}
			if isUID {
				log.Printf("Assuming '%s' is a UID for dashboard fetching.", dashName)
				return baseURL + "/api/dashboards/uid/" + dashName
			} else {
				log.Printf("Assuming '%s' is a slug for dashboard fetching.", dashName)
				return baseURL + "/api/dashboards/db/" + dashName
			}
		},
		getPanelEndpoint: func(dashName string, vals url.Values) string {
			renderURL := baseURL + "/render/d-solo/" + dashName + "?" + vals.Encode()
			return renderURL
		},
		apiToken:      apiToken,
		variables:     variables,
		sslCheck:      sslCheck,
		useGridLayout: gridLayout,
	}
}

// UsesGridLayout (Keep as is)
func (g *client) UsesGridLayout() bool {
	return g.useGridLayout
}

// GetDashboard (Keep as is)
func (g *client) GetDashboard(dashName string) (Dashboard, error) {
	dashURL := g.getDashEndpoint(dashName)
	log.Println("Getting dashboard definition from:", dashURL)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !g.sslCheck},
	}
	httpClient := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", dashURL, nil)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error creating GetDashboard request for %v: %w", dashURL, err)
	}
	if g.apiToken != "" {
		req.Header.Add("Authorization", "Bearer "+g.apiToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error executing GetDashboard request for %v: %w", dashURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return Dashboard{}, fmt.Errorf("error getting dashboard %v: Status %d, Body: %s", dashURL, resp.StatusCode, limitString(string(bodyBytes), 500))
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error reading GetDashboard response body for %v: %w", dashURL, err)
	}

	var fullDash FullDashboard
	err = json.Unmarshal(body, &fullDash)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error unmarshaling dashboard JSON from %v: %w\nRaw JSON response snippet:\n%s", dashURL, err, limitString(string(body), 500))
	}

	if fullDash.Dashboard.Uid == "" {
	    isUID := false
        if len(dashName) > 8 {
            isUID = true
            for _, r := range dashName {
                if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
                    isUID = false
                    break
                }
            }
        }
        if isUID {
            log.Printf("Dashboard JSON missing UID, using provided '%s' as UID.", dashName)
            fullDash.Dashboard.Uid = dashName
        } else {
             log.Printf("Warning: Dashboard JSON missing UID and provided name '%s' doesn't look like a UID.", dashName)
             if fullDash.Meta.Slug != "" {
                 log.Printf("Using dashboard slug '%s' as fallback identifier.", fullDash.Meta.Slug)
                 fullDash.Dashboard.Uid = fullDash.Meta.Slug
             } else {
                 fullDash.Dashboard.Uid = dashName
             }
        }
	}

	// Process panels and rows within the Dashboard struct
	fullDash.Dashboard.processPanelsAndRows()

	log.Printf("Successfully fetched dashboard: %s (UID: %s)", fullDash.Dashboard.Title, fullDash.Dashboard.Uid)
	return fullDash.Dashboard, nil
}

// Helper to limit string length for logging
func limitString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// GetPanelPng fetches a panel's PNG image (Keep as is)
func (g *client) GetPanelPng(p Panel, dashUID string, t TimeRange) (io.ReadCloser, error) {
	if dashUID == "" {
		return nil, fmt.Errorf("error rendering panel %d: dashboard UID is empty", p.Id)
	}
	// Construct URL parameters
	vals := url.Values{}
	vals.Add("panelId", strconv.Itoa(p.Id))
	vals.Add("width", "1000")
	vals.Add("height", "500")
	vals.Add("tz", "UTC")
	vals.Add("from", t.From)
	vals.Add("to", t.To)

	// Add dashboard variables
	for k, v := range g.variables {
		for _, singleV := range v {
			key := k
			if len(key) < 4 || key[:4] != "var-" {
				key = "var-" + key
			}
			vals.Add(key, singleV)
		}
	}

	// Generate the final render URL using the correct endpoint function
	endpointFunc := g.getPanelEndpoint // Get the function assigned during client creation
	renderURL := endpointFunc(dashUID, vals)
	log.Printf("Requesting panel '%s' (ID: %d) image using endpoint for UID '%s': %s", p.Title, p.Id, dashUID, renderURL)

	// Make the HTTP request with retries
	resp, err := g.makeRenderRequest(renderURL, p.Id, "panel")
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// makeRenderRequest (Keep as is, with increased timeout)
func (g *client) makeRenderRequest(renderURL string, id int, renderType string) (*http.Response, error) {
	var resp *http.Response
	var err error

	// Configure HTTP client
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !g.sslCheck},
	}
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("redirect detected for render URL %s (possible auth/token issue?)", req.URL)
		},
		Transport: tr,
		Timeout:   renderRequestTimeout, // Use timeout constant
	}

	// Create request
	req, err := http.NewRequest("GET", renderURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating render request for %s ID %d URL %v: %w", renderType, id, renderURL, err)
	}
	if g.apiToken != "" {
		req.Header.Add("Authorization", "Bearer "+g.apiToken)
	}
	req.Header.Add("User-Agent", "grafana-reporter-go")

	// Execute request with retries
	for retries := 0; retries <= maxGetPanelRetries; retries++ {
		if retries > 0 {
			delay := getPanelRetrySleepTime * time.Duration(retries)
			log.Printf("Retrying %s render for ID %d after %v...", renderType, id, delay)
			time.Sleep(delay)
		}

		resp, err = client.Do(req)
		if err != nil {
			if urlErr, ok := err.(*url.Error); ok && urlErr.Timeout() {
				log.Printf("Timeout error executing render request for %s ID %d (attempt %d/%d): %v", renderType, id, retries+1, maxGetPanelRetries+1, err)
			} else {
				log.Printf("Error executing render request for %s ID %d (attempt %d/%d): %v", renderType, id, retries+1, maxGetPanelRetries+1, err)
			}
			if retries == maxGetPanelRetries {
				return nil, fmt.Errorf("error executing render request for %s ID %d URL %v after %d retries: %w", renderType, id, renderURL, maxGetPanelRetries, err)
			}
			continue
		}

		// Check status code
		if resp.StatusCode == http.StatusOK {
			log.Printf("Successfully obtained render for %s ID %d (Status: %d)", renderType, id, resp.StatusCode)
			return resp, nil // Success!
		}

		// Handle non-OK status codes
		log.Printf("Error obtaining render for %s ID %d (attempt %d/%d), Status: %d", renderType, id, retries+1, maxGetPanelRetries+1, resp.StatusCode)
		bodyBytes, readErr := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
		    log.Printf("Failed to read response body after error status %d: %v", resp.StatusCode, readErr)
		}
		log.Printf("Response Body Snippet: %s", limitString(string(bodyBytes), 200))

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("error rendering %s ID %d: Not Found (404). Check dashboard UID/slug and %s ID. URL: %s", renderType, id, renderType, renderURL)
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("error rendering %s ID %d: Authorization Failed (%d). Check API token permissions. URL: %s. Body: %s", renderType, id, resp.StatusCode, renderURL, limitString(string(bodyBytes), 200))
		}
		if resp.StatusCode >= 500 {
			log.Printf("Server error (%d), will retry...", resp.StatusCode)
		} else {
			return nil, fmt.Errorf("error rendering %s ID %d: Client Error Status %d. URL: %s. Body: %s", renderType, id, resp.StatusCode, renderURL, limitString(string(bodyBytes), 200))
		}

		if retries == maxGetPanelRetries {
			return nil, fmt.Errorf("error rendering %s ID %d after %d retries: Last status %d. URL: %s. Body: %s", renderType, id, maxGetPanelRetries, resp.StatusCode, renderURL, limitString(string(bodyBytes), 200))
		}
	} // End retry loop

	return nil, fmt.Errorf("unexpected exit from render retry loop for %s ID %d", renderType, id)
}
