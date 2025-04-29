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
	"strings"
	"time"
)

// Client is a Grafana API client
type Client interface {
	GetDashboard(dashName string) (Dashboard, error)
	GetPanelPng(p Panel, dashName string, t TimeRange) (io.ReadCloser, error)
	GetRowPng(row Row, dashName string, t TimeRange, rowNum int) (io.ReadCloser, error)
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
	values := url.Values{}
	values.Add("theme", "light")
	values.Add("panelId", strconv.Itoa(p.Id))
	values.Add("from", t.From)
	values.Add("to", t.To)
	
	if g.gridLayout {
		width := int(p.GridPos.W * 40)
		height := int(p.GridPos.H * 40)
		values.Add("width", strconv.Itoa(width))
		values.Add("height", strconv.Itoa(height))
	} else {
		switch {
		case p.Is(SingleStat):
			values.Add("width", "300")
			values.Add("height", "150")
		case p.Is(Text):
			values.Add("width", "1000")
			values.Add("height", "100")
		default:
			values.Add("width", "1000")
			values.Add("height", "500")
		}
	}
	
	for k, v := range g.variables {
		for _, singleValue := range v {
			values.Add(k, singleValue)
		}
	}
	
	renderURL := g.getPanelEndpoint(dashName, values)
	log.Println("Downloading image", p.Id, renderURL)
	
	return renderURL
}

// GetRowPng captures a specific section of the dashboard based on the row number
// The rowNum parameter is used to scroll to the correct part of the dashboard
func (g client) GetRowPng(row Row, dashName string, t TimeRange, rowNum int) (io.ReadCloser, error) {
	// Determine the appropriate URL for the Grafana version
	var baseURL string
	if strings.Contains(g.getPanelEndpoint("test", url.Values{}), "render/d-solo") {
		// Grafana v5+
		baseURL = fmt.Sprintf("%s/render/d/%s", g.url, dashName)
	} else {
		// Grafana v4
		baseURL = fmt.Sprintf("%s/render/dashboard/db/%s", g.url, dashName)
	}
	
	// Create the query parameters
	values := url.Values{}
	values.Add("theme", "light")
	values.Add("from", t.From)
	values.Add("to", t.To)
	values.Add("width", "1400")    // Wide width for landscape format
	values.Add("height", "500")    // Height that should capture a single row
	
	// Use the height param to capture just one row - multiply by row number to "scroll"
	// Each row takes approximately 300px height, so we can calculate
	// the vertical offset based on the row number
	yOffset := rowNum * 300
	if rowNum > 0 {
		// For rows after the first, we need to add the right params to "scroll" down
		values.Add("render", "true")
		values.Add("viewport", fmt.Sprintf("1400x500@0,%d", yOffset))
	}
	
	// Add variables
	for k, v := range g.variables {
		for _, singleValue := range v {
			values.Add(k, singleValue)
		}
	}
	
	renderURL := fmt.Sprintf("%s?%s", baseURL, values.Encode())
	log.Printf("Downloading row %d image, URL: %s", rowNum, renderURL)
	
	// Make the HTTP request
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
	
	// Handle retries for non-200 status codes
	for retries := 1; retries < 3 && resp.StatusCode != 200; retries++ {
		delay := getPanelRetrySleepTime * time.Duration(retries)
		log.Printf("Error obtaining render for row %d, Status: %v, Retrying after %v...", rowNum, resp.StatusCode, delay)
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
