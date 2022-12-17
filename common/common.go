package common

import "path/filepath"

var ConfPath string
var ImagePath string
var ServicePath string
var UnitPath string
var VolumePath string

var baseDir string = "/var/lib/sloop"

func SetPaths(confDir string) {
	ConfPath = filepath.Join(confDir, "")
	ImagePath = filepath.Join(baseDir, "images")
	ServicePath = filepath.Join(baseDir, "services")
	UnitPath = filepath.Join(baseDir, "units")
	VolumePath = filepath.Join(baseDir, "volumes")
}
