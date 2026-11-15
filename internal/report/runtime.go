package report

import (
	"errors"
	"os/exec"
	"regexp"
)

var (
	rCodeChunk      = regexp.MustCompile("(?m)^```\\s*\\{\\s*r(?:\\s|[,}])")
	pythonCodeChunk = regexp.MustCompile("(?m)^```\\s*\\{\\s*python(?:\\s|[,}])")
)

type ScriptRequirements struct {
	NeedsR      bool `json:"needsR"`
	NeedsPython bool `json:"needsPython"`
}

func DetectRequirements(content string) ScriptRequirements {
	return ScriptRequirements{
		NeedsR:      rCodeChunk.MatchString(content),
		NeedsPython: pythonCodeChunk.MatchString(content),
	}
}

func validateScriptRuntime(content string) error {
	requirements := DetectRequirements(content)
	if requirements.NeedsR {
		if _, err := exec.LookPath("Rscript"); err != nil {
			return errors.New("qmd contains R code but Rscript is unavailable; install R or use the {{table}} data bridge")
		}
	}
	if requirements.NeedsPython {
		if _, err := exec.LookPath("jupyter"); err != nil {
			return errors.New("qmd contains Python code but Jupyter is unavailable; configure Quarto/Jupyter or use the {{table}} data bridge")
		}
	}
	return nil
}
