package common

import "path/filepath"

var ConfPath string
var ImagePath string
var UnitPath string
var VolumePath string

func SetPaths(baseDir string) {
	ConfPath = filepath.Join(baseDir, "")
	ImagePath = filepath.Join(baseDir, ".images")
	UnitPath = filepath.Join(baseDir, ".units")
	VolumePath = filepath.Join(baseDir, ".volumes")
}
