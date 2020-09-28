package vproj

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
)

// NewProject intializes a new project that vorteil is able to run
func NewProject(path string, flagVCFG *vcfg.VCFG, logger elog.View) error {
	logger.Printf("Creating project at '%s'", path)

	vcfgPath := filepath.Join(path, "default.vcfg")
	projectPath := filepath.Join(path, ".vorteilproject")

	err := createVCFGFile(vcfgPath, flagVCFG)
	if err != nil {
		return err
	}

	err = createProjectFile(projectPath)
	if err != nil {
		return err
	}

	return nil
}

func createVCFGFile(path string, fvcfg *vcfg.VCFG) error {
	// Open vcfg path so we can marshal to it
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := fvcfg.Marshal()
	if err != nil {
		return err
	}
	io.Copy(file, bytes.NewBuffer(data))
	return nil
}

func createProjectFile(path string) error {
	// Open project path so we can create that
	project, err := os.Create(path)
	if err != nil {
		return err
	}
	defer project.Close()
	pData := genericProjectData()
	data, err := pData.Marshal()
	if err != nil {
		return err
	}
	io.Copy(project, bytes.NewBuffer(data))
	return nil
}
