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

package report

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/IzakMarais/reporter/grafana"
	"github.com/pborman/uuid"
)

// Report interface (keep as is)
type Report interface {
	Generate() (pdf io.ReadCloser, err error)
	Title() string
	Clean()
}

// report struct (keep as is)
type report struct {
	gClient      grafana.Client
	time         grafana.TimeRange
	texTemplate  string
	dashName     string
	tmpDir       string
	dashTitle    string
	useRowLayout bool
}

// Constants (keep as is)
const (
	imgDir        = "images"
	reportTexFile = "report.tex"
	reportPdfFile = "report.pdf"
	logFile       = "pdflatex.log"
)

// New function (keep as is)
func New(g grafana.Client, dashName string, time grafana.TimeRange, texTemplatePath string, useRowLayout bool) Report {
	tmpDir := filepath.Join(os.TempDir(), "reporter", uuid.New())
	log.Println("Report temporary directory:", tmpDir)

	var templateContent string
	if texTemplatePath != "" {
		log.Println("Using custom template:", texTemplatePath)
		content, err := ioutil.ReadFile(texTemplatePath)
		if err != nil {
			log.Printf("Warning: Failed to read custom template '%s': %v. Falling back to default.", texTemplatePath, err)
			if useRowLayout {
				templateContent = rowBasedTemplate
			} else {
				templateContent = defaultTemplate
			}
		} else {
			templateContent = string(content)
		}
	} else {
		if useRowLayout {
			log.Println("Using built-in row-based template.")
			templateContent = rowBasedTemplate
		} else {
			log.Println("Using built-in grid-based template.")
			templateContent = defaultTemplate
		}
	}

	return &report{
		gClient:      g,
		time:         time,
		texTemplate:  templateContent,
		dashName:     dashName,
		tmpDir:       tmpDir,
		useRowLayout: useRowLayout,
	}
}

// Title function (keep as is)
func (rep *report) Title() string {
	return rep.dashTitle
}

// Generate function (keep as is)
func (rep *report) Generate() (pdf io.ReadCloser, err error) {
	dash, err := rep.gClient.GetDashboard(rep.dashName)
	if err != nil {
		rep.Clean()
		return nil, fmt.Errorf("error getting dashboard: %v", err)
	}
	rep.dashTitle = dash.Title
	dashUID := dash.Uid
	if dashUID == "" {
		log.Printf("Warning: Dashboard UID is empty after fetching '%s'. Rendering might fail.", rep.dashName)
		dashUID = rep.dashName
	}

	err = rep.fetchImages(dash, dashUID)
	if err != nil {
		rep.Clean()
		return nil, fmt.Errorf("error fetching panel images: %v", err)
	}

	err = rep.createTex(dash)
	if err != nil {
		rep.Clean()
		return nil, fmt.Errorf("error creating tex file: %v (temp dir: %s)", err, rep.tmpDir)
	}

	pdfFile, err := rep.runLaTeX()
	if err != nil {
		log.Printf("LaTeX failed. Temporary files are in %s", rep.tmpDir)
		return nil, fmt.Errorf("error running LaTeX: %v", err)
	}

	return pdfFile, nil
}

// Clean function (keep as is)
func (rep *report) Clean() {
	err := os.RemoveAll(rep.tmpDir)
	if err != nil {
		log.Printf("Warning: Could not clean up temporary directory '%s': %v", rep.tmpDir, err)
	} else {
		log.Println("Cleaned up temporary directory:", rep.tmpDir)
	}
}

// Path helpers (keep as is)
func (rep *report) imgDirPath() string {
	return filepath.Join(rep.tmpDir, imgDir)
}
func (rep *report) imgFilePath(panelID int) string {
	fileName := fmt.Sprintf("image%d.png", panelID)
	return filepath.Join(rep.imgDirPath(), fileName)
}
func (rep *report) rowImgFilePath(rowID int) string {
	fileName := fmt.Sprintf("row%d.png", rowID)
	return filepath.Join(rep.imgDirPath(), fileName)
}
func (rep *report) texPath() string {
	return filepath.Join(rep.tmpDir, reportTexFile)
}
func (rep *report) pdfPath() string {
	return filepath.Join(rep.tmpDir, reportPdfFile)
}
func (rep *report) logPath() string {
	return filepath.Join(rep.tmpDir, logFile)
}

// fetchImages function (keep as is)
func (rep *report) fetchImages(dash grafana.Dashboard, dashUID string) error {
	imgDirPath := rep.imgDirPath()
	err := os.MkdirAll(imgDirPath, 0777)
	if err != nil {
		return fmt.Errorf("error creating image directory at %v: %v", imgDirPath, err)
	}

	var wg sync.WaitGroup
	errorChannel := make(chan error, 100)
	log.Println("Downloading images...")

	if rep.useRowLayout {
		rowsToProcess := dash.GetRows()
		if len(rowsToProcess) == 0 {
			log.Println("Warning: Row layout selected, but no rows found to process.")
			return nil
		}
		log.Printf("Fetching images for panels within %d rows...", len(rowsToProcess))
		panelCount := 0
		for _, row := range rowsToProcess {
			log.Printf("Processing panels for row %d ('%s')", row.Id, row.Title)
			for _, p := range row.ContentPanels {
				if p.Type == "text" {
					log.Printf("Skipping image download for text panel in row %d: %d (%s)", row.Id, p.Id, p.Title)
					continue
				}
				panelCount++
				wg.Add(1)
				go func(panel grafana.Panel) {
					defer wg.Done()
					err := rep.downloadPanelImage(panel, dashUID)
					if err != nil {
						log.Printf("Warning: Failed to download image for panel %d ('%s'): %v", panel.Id, panel.Title, err)
						errorChannel <- fmt.Errorf("panel %d ('%s'): %w", panel.Id, panel.Title, err)
					}
				}(p)
			}
		}
		log.Printf("Scheduled downloads for %d panels across rows.", panelCount)
	} else {
		panelsToFetch := dash.GetGridPanels()
		if len(panelsToFetch) == 0 {
			log.Println("Warning: Grid layout selected, but no panels found to process.")
			return nil
		}
		log.Printf("Fetching images for %d panels (grid layout)...", len(panelsToFetch))
		for _, p := range panelsToFetch {
			if p.Type == "text" {
				log.Printf("Skipping image download for text panel: %d (%s)", p.Id, p.Title)
				continue
			}
			wg.Add(1)
			go func(panel grafana.Panel) {
				defer wg.Done()
				err := rep.downloadPanelImage(panel, dashUID)
				if err != nil {
					log.Printf("Warning: Failed to download image for panel %d ('%s'): %v", panel.Id, panel.Title, err)
					errorChannel <- fmt.Errorf("panel %d ('%s'): %w", panel.Id, panel.Title, err)
				}
			}(p)
		}
	}

	wg.Wait()
	close(errorChannel)

	var downloadErrors []string
	for err := range errorChannel {
		downloadErrors = append(downloadErrors, err.Error())
	}
	if len(downloadErrors) > 0 {
		log.Printf("Finished downloading images with %d error(s). Report generation will continue.\n- %s",
			len(downloadErrors), strings.Join(downloadErrors, "\n- "))
	} else {
		log.Println("Finished downloading images successfully.")
	}
	return nil
}

// downloadPanelImage function (keep as is)
func (rep *report) downloadPanelImage(p grafana.Panel, dashUID string) error {
	imgPath := rep.imgFilePath(p.Id)
	log.Printf("Downloading panel %d ('%s') image to %s...", p.Id, p.Title, imgPath)

	body, err := rep.gClient.GetPanelPng(p, dashUID, rep.time)
	if err != nil {
		return err
	}
	defer body.Close()

	file, err := os.Create(imgPath)
	if err != nil {
		return fmt.Errorf("error creating image file %v: %v", imgPath, err)
	}
	defer file.Close()

	_, err = io.Copy(file, body)
	if err != nil {
		_ = os.Remove(imgPath)
		return fmt.Errorf("error writing image file %v: %v", imgPath, err)
	}
	log.Printf("Done downloading panel %d.", p.Id)
	return nil
}

// formatVariables function (keep as is)
func formatVariables(variables []grafana.TemplateVariable) string {
	var parts []string
	for _, v := range variables {
		if v.Hide == 2 { continue }
		currentValStr := ""
		if v.Current.Text != nil {
			switch text := v.Current.Text.(type) {
			case string: currentValStr = text
			case []interface{}:
				var vals []string
				for _, item := range text { vals = append(vals, fmt.Sprintf("%v", item)) }
				if v.IncludeAll && v.Current.Value == "$__all" || (len(vals) > 1 && v.Current.Text == "All") {
					currentValStr = "All"
				} else { currentValStr = strings.Join(vals, ", ") }
			default: currentValStr = fmt.Sprintf("%v", v.Current.Text)
			}
		} else if v.Current.Value != "" {
			if strings.HasPrefix(v.Current.Value, "[") && strings.HasSuffix(v.Current.Value, "]") && v.Multi {
				vals := strings.TrimSuffix(strings.TrimPrefix(v.Current.Value, "["), "]")
				currentValStr = strings.ReplaceAll(vals, "\",\"", ", ")
				currentValStr = strings.ReplaceAll(currentValStr, "\"", "")
			} else { currentValStr = v.Current.Value }
		}
		if v.Hide == 1 { currentValStr = "" }
		label := v.Name
		if v.Label != "" { label = v.Label }
		if currentValStr != "" { parts = append(parts, fmt.Sprintf("%s: %s", label, currentValStr))
		} else { parts = append(parts, label) }
	}
	return strings.Join(parts, "; ")
}

// createTex function - **MODIFIED templData and data population**
func (rep *report) createTex(dash grafana.Dashboard) error {
	// Define functions for the template (keep EscapeLaTeX and PanelImagePath)
	funcMap := template.FuncMap{
		"EscapeLaTeX": grafana.SanitizeLaTexInput,
		"PanelImagePath": func(panelID int) string {
			return fmt.Sprintf("%s/image%d.png", imgDir, panelID)
		},
		// Remove other helpers if not needed or ensure they work without funcMap context
	}

	// **MODIFIED templData struct:**
	type templData struct {
		// Keep essential top-level info
		Title          string
		Description    string
		VariableValues string
		ImgDir         string
		FromFormatted  string
		ToFormatted    string
		UseRowLayout   bool
		// Add explicit fields for Rows and Panels
		Rows   []grafana.GrafanaRow
		Panels []grafana.Panel
	}

	// **Populate the explicit fields:**
	data := templData{
		Title:          dash.Title,       // Use title from dashboard struct
		Description:    dash.Description, // Use description from dashboard struct
		VariableValues: formatVariables(dash.Templating.List),
		ImgDir:         imgDir,
		FromFormatted:  rep.time.From,
		ToFormatted:    rep.time.To,
		UseRowLayout:   rep.useRowLayout,
		// Call the methods on the dash object to get the processed data
		Rows:   dash.GetRows(),
		Panels: dash.GetGridPanels(),
	}

	// Create directory if it doesn't exist
	err := os.MkdirAll(rep.tmpDir, 0777)
	if err != nil {
		return fmt.Errorf("error creating temporary directory at %v: %v", rep.tmpDir, err)
	}

	// Create the .tex file
	texPath := rep.texPath()
	file, err := os.Create(texPath)
	if err != nil {
		return fmt.Errorf("error creating tex file at %v : %v", texPath, err)
	}
	defer file.Close()

	// Parse the template content
	tmplName := filepath.Base(texPath)
	tmpl, err := template.New(tmplName).Funcs(funcMap).Delims("[[", "]]").Parse(rep.texTemplate)
	if err != nil {
		templateSample := rep.texTemplate
		maxSampleLength := 500
		if len(templateSample) > maxSampleLength {
			templateSample = templateSample[:maxSampleLength] + "..."
		}
		return fmt.Errorf("error parsing template (%s): %v\nTemplate content sample:\n%s\n(temp dir: %s)", tmplName, err, templateSample, rep.tmpDir)
	}

	// Execute the template with the modified data struct
	err = tmpl.Execute(file, data)
	if err != nil {
		return fmt.Errorf("error executing tex template (%s): %v (temp dir: %s)", tmplName, err, rep.tmpDir)
	}

	log.Println("Created LaTeX file:", texPath)
	return nil
}


// runLaTeX function (Keep as is)
func (rep *report) runLaTeX() (pdf *os.File, err error) {
	imgDirPath := rep.imgDirPath()
	if _, errStat := os.Stat(imgDirPath); os.IsNotExist(errStat) {
		return nil, fmt.Errorf("image directory '%s' not found before running LaTeX. Check fetchImages logs.", imgDirPath)
	} else {
		files, _ := ioutil.ReadDir(imgDirPath)
		if len(files) == 0 {
			log.Printf("Warning: Image directory '%s' exists but is empty. LaTeX compilation might fail to find images.", imgDirPath)
		}
	}

	texPath := rep.texPath()
	texFileBase := filepath.Base(texPath)
	pdfPath := rep.pdfPath()
	logPath := rep.logPath()

	for i := 1; i <= 2; i++ {
		args := []string{"-interaction=nonstopmode", "-halt-on-error", texFileBase}
		cmd := exec.Command("pdflatex", args...)
		cmd.Dir = rep.tmpDir
		log.Printf("Running LaTeX command (pass %d)... Command: %s, Dir: %s", i, cmd.String(), cmd.Dir)

		outBytes, errCmd := cmd.CombinedOutput()
		logErr := ioutil.WriteFile(logPath, outBytes, 0666)
		if logErr != nil {
			log.Printf("Warning: Failed to write pdflatex output log to %s: %v", logPath, logErr)
		}

		if errCmd != nil {
			outputHint := string(outBytes)
			maxLogTail := 2000
			if len(outputHint) > maxLogTail {
				outputHint = "... (last " + fmt.Sprint(maxLogTail) + " chars)\n" + outputHint[len(outputHint)-maxLogTail:]
			}
			return nil, fmt.Errorf("error running LaTeX (pass %d): %v. Output logged to %s\n-- LaTeX Output Tail --\n%s\n-- LaTeX Output End --", i, errCmd, logPath, outputHint)
		}
		log.Printf("LaTeX pass %d completed successfully.", i)
	}

	if _, errStat := os.Stat(pdfPath); os.IsNotExist(errStat) {
		logContent, _ := ioutil.ReadFile(logPath)
		logContentStr := string(logContent)
		maxLogTail := 2000
		if len(logContentStr) > maxLogTail {
			logContentStr = "... (last " + fmt.Sprint(maxLogTail) + " chars)\n" + logContentStr[len(logContentStr)-maxLogTail:]
		}
		return nil, fmt.Errorf("error: LaTeX completed but PDF file '%s' not found. Check LaTeX logs in %s\nLog Content Tail:\n%s", pdfPath, rep.tmpDir, logContentStr)
	}

	log.Println("Created PDF file:", pdfPath)
	pdfFile, err := os.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("error opening PDF file '%s': %v", pdfPath, err)
	}
	return pdfFile, nil
}
