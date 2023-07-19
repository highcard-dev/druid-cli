package services

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/sprig"
)

type TemplateRenderer struct{}

func NewTemplateRenderer() *TemplateRenderer {
	return &TemplateRenderer{}
}

func (tr *TemplateRenderer) RenderTemplate(templatePath string, data interface{}) (string, error) {
	tmpl, err := template.New("scroll_template").Funcs(sprig.TxtFuncMap()).Parse(templatePath)
	if err != nil {
		return "", err
	}

	var tpl bytes.Buffer
	err = tmpl.Execute(&tpl, data)

	if err != nil {
		return "", err
	}

	return tpl.String(), err
}

func (tr *TemplateRenderer) RenderScrollTemplateFiles(templateFiles []string, data any, outputDir string) error {

	// Parse the template files
	templates, err := template.New("scroll_template").Funcs(sprig.TxtFuncMap()).ParseFiles(templateFiles...)
	if err != nil {
		return err
	}

	for _, templateFile := range templateFiles {
		// Remove the "template" suffix from the file name
		outputFileName := strings.TrimSuffix(templateFile, ".scroll_template")

		if outputDir != "" {
			filename := filepath.Base(outputFileName)

			// Prepend the output directory if specified
			outputFileName = filepath.Join(outputDir, filename)
		}

		// Create a new file for the rendered output
		outputFile, err := os.Create(outputFileName)
		if err != nil {
			return err
		}
		defer outputFile.Close()

		// Execute the template and write the output to the file
		err = templates.Funcs(sprig.FuncMap()).ExecuteTemplate(outputFile, filepath.Base(templateFile), data)
		if err != nil {
			return err
		}
	}
	return nil
}
