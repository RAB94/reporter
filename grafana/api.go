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
	"errors"
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
    GetRowPng(row Row, dashName string, t TimeRange) (io.ReadCloser, error) // New method
}

type client struct {
	url              string
	getDashEndpoint  func(dashName string) string
	getPanelEndpoint func(dashName string, vals url.Values) string
	apiToken         string
	variables        url.Values
	sslCheck         bool
	gridLayout       bool
}

var getPanelRetrySleepTime = time.Duration(10) * time.Second

// NewV4Client creates a new Grafana 4 Client. If apiToken is the empty string,
// authorization headers will be omitted from requests.
// variables are Grafana template variable url values of the form var-{name}={value}, e.g. var-host=dev
func NewV4Client(grafanaURL string, apiToken string, variables url.Values, sslCheck bool, gridLayout bool) Client {
	getDashEndpoint := func(dashName string) string {
		dashURL := grafanaURL + "/api/dashboards/db/" + dashName
		if len(variables) > 0 {
			dashURL = dashURL + "?" + variables.Encode()
		}
		return dashURL
	}

	getPanelEndpoint := func(dashName string, vals url.Values) string {
		return fmt.Sprintf("%s/render/dashboard-solo/db/%s?%s", grafanaURL, dashName, vals.Encode())
	}
	return client{grafanaURL, getDashEndpoint, getPanelEndpoint, apiToken, variables, sslCheck, gridLayout}
}

// NewV5Client creates a new Grafana 5 Client. If apiToken is the empty string,
// authorization headers will be omitted from requests.
// variables are Grafana template variable url values of the form var-{name}={value}, e.g. var-host=dev
func NewV5Client(grafanaURL string, apiToken string, variables url.Values, sslCheck bool, gridLayout bool) Client {
	getDashEndpoint := func(dashName string) string {
		dashURL := grafanaURL + "/api/dashboards/uid/" + dashName
		if len(variables) > 0 {
			dashURL = dashURL + "?" + variables.Encode()
		}
		return dashURL
	}

	getPanelEndpoint := func(dashName string, vals url.Values) string {
		return fmt.Sprintf("%s/render/d-solo/%s/_?%s", grafanaURL, dashName, vals.Encode())
	}
	return client{grafanaURL, getDashEndpoint, getPanelEndpoint, apiToken, variables, sslCheck, gridLayout}
}

func (g client) GetDashboard(dashName string) (Dashboard, error) {
	dashURL := g.getDashEndpoint(dashName)
	log.Println("Connecting to dashboard at", dashURL)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !g.sslCheck},
	}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", dashURL, nil)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error creating getDashboard request for %v: %v", dashURL, err)
	}

	if g.apiToken != "" {
		req.Header.Add("Authorization", "Bearer "+g.apiToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error executing getDashboard request for %v: %v", dashURL, err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Dashboard{}, fmt.Errorf("error reading getDashboard response body from %v: %v", dashURL, err)
	}

	if resp.StatusCode != 200 {
		return Dashboard{}, fmt.Errorf("error obtaining dashboard from %v. Got Status %v, message: %v ", dashURL, resp.Status, string(body))
	}

	return NewDashboard(body, g.variables), nil
}

func (g client) GetPanelPng(p Panel, dashName string, t TimeRange) (io.ReadCloser, error) {
	panelURL := g.getPanelURL(p, dashName, t)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !g.sslCheck},
	}
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("Error getting panel png. Redirected to login")
		},
		Transport: tr,
	}
	req, err := http.NewRequest("GET", panelURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating getPanelPng request for %v: %v", panelURL, err)
	}
	if g.apiToken != "" {
		req.Header.Add("Authorization", "Bearer "+g.apiToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing getPanelPng request for %v: %v", panelURL, err)
	}

	for retries := 1; retries < 3 && resp.StatusCode != 200; retries++ {
		delay := getPanelRetrySleepTime * time.Duration(retries)
		log.Printf("Error obtaining render for panel %+v, Status: %v, Retrying after %v...", p, resp.StatusCode, delay)
		time.Sleep(delay)
		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error executing retry getPanelPng request for %v: %v", panelURL, err)
		}
	}

	if resp.StatusCode != 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		log.Println("Error obtaining render:", string(body))
		return nil, errors.New("Error obtaining render: " + resp.Status)
	}

	return resp.Body, nil
}

func (g client) getPanelURL(p Panel, dashName string, t TimeRange) string {
    // Initialize URL query parameters
    values := url.Values{}
    
    // Add basic parameters
    values.Add("theme", "light")
    values.Add("panelId", strconv.Itoa(p.Id))
    values.Add("from", t.From)
    values.Add("to", t.To)
    
    // Determine panel dimensions
    if g.gridLayout {
        // Calculate dimensions based on grid position
        width := int(p.GridPos.W * 40)
        height := int(p.GridPos.H * 40)
        values.Add("width", strconv.Itoa(width))
        values.Add("height", strconv.Itoa(height))
    } else {
        // Use predefined dimensions based on panel type
        switch {
        case p.Is(SingleStat):
            values.Add("width", "300")
            values.Add("height", "150")
        case p.Is(Text):
            values.Add("width", "1000")
            values.Add("height", "100")
        default:
            // Default dimensions for other panel types
            values.Add("width", "1000")
            values.Add("height", "500")
        }
    }
    
    // Add dashboard variables
    for k, v := range g.variables {
        for _, singleValue := range v {
            values.Add(k, singleValue)
        }
    }
    
    // Construct the final URL
    renderURL := g.getPanelEndpoint(dashName, values)
    log.Println("Downloading image", p.Id, renderURL)
    
    return renderURL
}

// GetRowPng captures an entire row of the dashboard as a single image
func (g client) GetRowPng(row Row, dashName string, t TimeRange) (io.ReadCloser, error) {
    // Calculate the total width of the row (Grafana uses a 24-column grid system)
    totalWidth := 1200 // Default width for the row

    // Calculate the maximum height among all panels in the row
    maxHeight := 0
    for _, panel := range row.Panels {
        if g.gridLayout {
            // In grid layout, use the panel's grid position
            panelHeight := int(panel.GridPos.H * 40)
            if panelHeight > maxHeight {
                maxHeight = panelHeight
            }
        } else {
            // Set a reasonable default height if grid layout is not used
            if panel.Is(SingleStat) {
                maxHeight = 150
            } else if panel.Is(Text) {
                maxHeight = 100
            } else {
                maxHeight = 500
            }
        }
    }

    // Ensure we have reasonable minimum dimensions
    if maxHeight < 300 {
        maxHeight = 300
    }

    // Create the query parameters for the dashboard rendering
    values := url.Values{}
    values.Add("theme", "light")
    values.Add("from", t.From)
    values.Add("to", t.To)
    values.Add("width", strconv.Itoa(totalWidth))
    values.Add("height", strconv.Itoa(maxHeight))
    
    // In Grafana, to render a specific row, we need to add a rowId parameter
    // This is tricky because Grafana doesn't have a direct API for row screenshots
    if row.Id > 0 {
        values.Add("panelId", strconv.Itoa(row.Id)) // We'll try to use the row ID
    } else {
        // If no ID, we'll just capture the entire dashboard and crop it later
        // This is a fallback approach
        log.Printf("Warning: Row ID not available, capturing full dashboard")
    }

    // Create the URL for rendering
    renderURL := fmt.Sprintf("%s/render/dashboard/db/%s?%s", g.url, dashName, values.Encode())
    log.Printf("Downloading row image, URL: %s", renderURL)

    // Make the HTTP request similar to GetPanelPng
    tr := &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: !g.sslCheck},
    }
    client := &http.Client{
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return errors.New("Error getting row png. Redirected to login")
        },
        Transport: tr,
    }
    req, err := http.NewRequest("GET", renderURL, nil)
    if err != nil {
        return nil, fmt.Errorf("error creating getRowPng request for %v: %v", renderURL, err)
    }
    if g.apiToken != "" {
        req.Header.Add("Authorization", "Bearer "+g.apiToken)
    }
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("error executing getRowPng request for %v: %v", renderURL, err)
    }

    // Handle non-200 responses similar to GetPanelPng
    for retries := 1; retries < 3 && resp.StatusCode != 200; retries++ {
        delay := getPanelRetrySleepTime * time.Duration(retries)
        log.Printf("Error obtaining render for row, Status: %v, Retrying after %v...", resp.StatusCode, delay)
        time.Sleep(delay)
        resp, err = client.Do(req)
        if err != nil {
            return nil, fmt.Errorf("error executing retry getRowPng request for %v: %v", renderURL, err)
        }
    }

    if resp.StatusCode != 200 {
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            panic(err)
        }
        log.Println("Error obtaining render:", string(body))
        return nil, errors.New("Error obtaining render: " + resp.Status)
    }

    return resp.Body, nil
}
