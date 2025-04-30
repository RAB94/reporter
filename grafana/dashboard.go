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
	"encoding/json" // Keep for unmarshaling panel/row JSON if needed later
	"log"
	"net/url"
	"sort"
	"strings" // Keep for getVariablesValues and sanitizeLaTexInput
)

// --- Panel Type Enum (Keep as is) ---
type PanelType int

const (
	SingleStat PanelType = iota
	Text
	Graph
	Table
	Row // Keep Row type for identification
)

func (p PanelType) string() string {
	return [...]string{
		"singlestat",
		"text",
		"graph",
		"table",
		"row", // Added "row" representation
	}[p]
}

// --- Struct Definitions ---

// FullDashboard represents the entire JSON structure returned by the Grafana API
// It includes metadata and the main dashboard definition.
type FullDashboard struct {
	Meta      DashboardMeta `json:"meta"`
	Dashboard Dashboard     `json:"dashboard"`
}

// DashboardMeta contains metadata about the dashboard. Add fields as needed.
type DashboardMeta struct {
	Slug    string `json:"slug"`
	Version int    `json:"version"`
	// Add other meta fields if required (e.g., FolderId, FolderTitle)
}

// Dashboard represents the main dashboard structure.
type Dashboard struct {
	Title       string            `json:"title"`
	Description string            `json:"description"` // Added Description field
	Uid         string            `json:"uid"`
	Time        Time              `json:"time"`
	Templating  Templating        `json:"templating"`
	Timezone    string            `json:"timezone"`
	Rows        []json.RawMessage `json:"rows"`   // Deprecated in Grafana v5+, use Panels directly
	Panels      []json.RawMessage `json:"panels"` // Grafana v5+ stores panels here, including rows
	// Internal fields to store processed panels/rows
	processedPanels []Panel
	processedRows   []GrafanaRow
}

// Time represents the dashboard's default time range
type Time struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Templating holds information about dashboard variables
type Templating struct {
	List []TemplateVariable `json:"list"`
}

// TemplateVariable represents a single dashboard variable
type TemplateVariable struct {
	Name       string      `json:"name"`
	Label      string      `json:"label"`    // Display name
	Type       string      `json:"type"`     // e.g., query, custom, interval
	Current    CurrentVal  `json:"current"`  // Selected value(s)
	Options    []OptionVal `json:"options"`  // Available options
	Multi      bool        `json:"multi"`    // Allow multiple selections?
	IncludeAll bool        `json:"includeAll"`// Has 'All' option?
	Hide       int         `json:"hide"`     // 0=visible, 1=label only, 2=hidden
	// Add other fields like 'query', 'datasource' if needed
}

// CurrentVal holds the currently selected value(s) for a template variable
type CurrentVal struct {
	Text  interface{} `json:"text"`  // Can be string or []string/[]interface{}
	Value string      `json:"value"` // Can be string or JSON array string '["val1","val2"]'
	// Add 'tags' or other fields if necessary
}

// OptionVal represents one available option for a template variable
type OptionVal struct {
	Text     string `json:"text"`
	Value    string `json:"value"`
	Selected bool   `json:"selected"`
}

// Panel represents common fields for Grafana panels (including rows)
type Panel struct {
	Id      int     `json:"id"`
	Type    string  `json:"type"` // "row", "graph", "singlestat", etc.
	Title   string  `json:"title"`
	GridPos GridPos `json:"gridPos"`

	// Fields specific to 'row' type panels:
	Collapsed bool              `json:"collapsed,omitempty"`
	Panels    []json.RawMessage `json:"panels,omitempty"` // Nested panels within a row

	// Internal cache for panels contained within this row panel
	ContentPanels []Panel `json:"-"` // Use json:"-" to prevent marshalling loops
}

// GridPos represents position and size in the Grafana grid
type GridPos struct {
	H float64 `json:"h"`
	W float64 `json:"w"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GrafanaRow represents a structured row, processed from Panel data
type GrafanaRow struct {
	Id            int
	Title         string
	Showtitle     bool // True if the row title should be displayed
	ContentPanels []Panel
	GridPos       GridPos // Position of the row itself
}

// --- Helper Functions ---

// processPanelsAndRows extracts Panels and Rows from the raw JSON messages
// This should be called after unmarshaling into FullDashboard
func (d *Dashboard) processPanelsAndRows() {
	if d.processedPanels != nil || d.processedRows != nil {
		return // Already processed
	}

	var allPanels []Panel
	panelSource := d.Panels // Use Panels field (Grafana v5+)

	// Fallback to Rows field if Panels is empty (older Grafana versions)
	if len(panelSource) == 0 && len(d.Rows) > 0 {
		log.Println("Using deprecated 'rows' field for panel data.")
		panelSource = d.Rows
	}

	log.Printf("Processing %d raw panel/row entries...", len(panelSource))
	currentY := -1.0 // Track Y coordinate to group panels into implicit rows if needed
	var currentRowPanels []Panel
	var explicitRows []GrafanaRow

	for _, raw := range panelSource {
		var p Panel
		err := json.Unmarshal(raw, &p)
		if err != nil {
			log.Printf("Warning: Skipping panel/row - Error unmarshaling panel JSON: %v. JSON: %s", err, limitString(string(raw), 100))
			continue
		}

		if p.Type == "row" {
			log.Printf("Processing Row: %s (ID: %d)", p.Title, p.Id)
			// Process nested panels within the row
			var nestedPanels []Panel
			for _, nestedRaw := range p.Panels {
				var nestedP Panel
				err := json.Unmarshal(nestedRaw, &nestedP)
				if err != nil {
					log.Printf("Warning: Skipping nested panel in row %d - Error unmarshaling: %v. JSON: %s", p.Id, err, limitString(string(nestedRaw), 100))
					continue
				}
				// Assign Y coordinate relative to row if needed, though GridPos usually handles it
				nestedPanels = append(nestedPanels, nestedP)
				allPanels = append(allPanels, nestedP) // Also add to the flat list
			}
			p.ContentPanels = nestedPanels // Store processed nested panels internally

			// Create a structured GrafanaRow
			explicitRows = append(explicitRows, GrafanaRow{
				Id:            p.Id,
				Title:         p.Title,
				Showtitle:     !p.Collapsed, // Consider collapsed rows as not showing title prominently
				ContentPanels: nestedPanels,
				GridPos:       p.GridPos,
			})
		} else {
			// Regular panel
			allPanels = append(allPanels, p)

			// Group panels by Y coordinate for implicit row generation if needed (less common now)
			if int(p.GridPos.Y) != int(currentY) {
				// Start of a new potential implicit row
				currentY = p.GridPos.Y
				currentRowPanels = []Panel{}
			}
			currentRowPanels = append(currentRowPanels, p)
		}
	}

	// Sort panels and rows primarily by Y coordinate, then X
	sort.SliceStable(allPanels, func(i, j int) bool {
		if allPanels[i].GridPos.Y != allPanels[j].GridPos.Y {
			return allPanels[i].GridPos.Y < allPanels[j].GridPos.Y
		}
		return allPanels[i].GridPos.X < allPanels[j].GridPos.X
	})
	sort.SliceStable(explicitRows, func(i, j int) bool {
		// Use row's GridPos.Y for sorting
		if explicitRows[i].GridPos.Y != explicitRows[j].GridPos.Y {
			return explicitRows[i].GridPos.Y < explicitRows[j].GridPos.Y
		}
		return explicitRows[i].GridPos.X < explicitRows[j].GridPos.X
	})

	d.processedPanels = allPanels
	d.processedRows = explicitRows // Store the processed rows
	log.Printf("Finished processing: %d panels, %d explicit rows identified.", len(d.processedPanels), len(d.processedRows))
}

// GetGridPanels returns panels suitable for grid layout (non-row panels)
// It ensures panels are processed first.
func (d *Dashboard) GetGridPanels() []Panel {
	d.processPanelsAndRows() // Ensure data is processed
	var gridPanels []Panel
	for _, p := range d.processedPanels {
		if p.Type != "row" { // Exclude panels that are actually row definitions
			gridPanels = append(gridPanels, p)
		}
	}
	return gridPanels
}

// GetRows returns processed rows suitable for row layout.
// It ensures panels/rows are processed first.
func (d *Dashboard) GetRows() []GrafanaRow {
	d.processPanelsAndRows() // Ensure data is processed
	return d.processedRows
}

// --- Methods for Panel and GrafanaRow (Keep existing ones) ---

func (p Panel) IsSingleStat() bool {
	return p.Is(SingleStat)
}

func (p Panel) IsPartialWidth() bool {
	return (p.GridPos.W < 24)
}

func (p Panel) Width() float64 {
	return float64(p.GridPos.W) * 0.04
}

func (p Panel) Height() float64 {
	return float64(p.GridPos.H) * 0.04
}

func (p Panel) Is(t PanelType) bool {
	if p.Type == t.string() {
		return true
	}
	return false
}

func (r GrafanaRow) IsVisible() bool {
	// Use Showtitle which is determined during processing
	return r.Showtitle
}

// --- Standalone Utility Functions (Keep as is) ---

func getVariablesValues(variables url.Values) string {
	values := []string{}
	for _, v := range variables {
		values = append(values, strings.Join(v, ", "))
	}
	return strings.Join(values, ", ")
}

// SanitizeLaTexInput escapes characters problematic for LaTeX
func SanitizeLaTexInput(input string) string {
	// Use a temporary placeholder for backslashes to avoid double escaping
	placeholder := "___TEMP_BACKSLASH___"
	input = strings.ReplaceAll(input, "\\", placeholder)

	// Escape other special characters
	input = strings.ReplaceAll(input, "&", "\\&")
	input = strings.ReplaceAll(input, "%", "\\%")
	input = strings.ReplaceAll(input, "$", "\\$")
	input = strings.ReplaceAll(input, "#", "\\#")
	input = strings.ReplaceAll(input, "_", "\\_")
	input = strings.ReplaceAll(input, "{", "\\{")
	input = strings.ReplaceAll(input, "}", "\\}")
	input = strings.ReplaceAll(input, "~", "\\textasciitilde{}") // Requires textcomp package? Or use verb
	input = strings.ReplaceAll(input, "^", "\\textasciicircum{}") // Requires textcomp package? Or use verb

	// Replace the placeholder with the LaTeX command for backslash
	// Ensure space after command if needed depending on context, but usually safe this way
	input = strings.ReplaceAll(input, placeholder, "\\textbackslash{}")

	return input
}

// Helper to limit string length for logging (already defined in api.go, but keep here for potential use within package)
// func limitString(s string, maxLen int) string {
// 	if len(s) <= maxLen {
// 		return s
// 	}
// 	return s[:maxLen] + "..."
// }
