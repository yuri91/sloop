package systemd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"yuri91/sloop/common"
	"yuri91/sloop/cue"
	"yuri91/sloop/image"

	"github.com/coreos/go-systemd/v22/dbus"
)


func handleVolume(v cue.Volume) error {
	if v.Name[0] == '/' {
		return nil
	}
	p := filepath.Join(common.VolumePath, v.Name)
	err := os.MkdirAll(p, 0777)
	if err != nil {
		return CreateVolumeError.Wrap(err, "cannot create volume directory %s", p)
	}
	return nil
}

func handleImage(i cue.Image) (bool, error) {
	parts := strings.SplitN(i.From, ":", 2)
	from := parts[0]
	ver := parts[1]

	p := filepath.Join(common.ImagePath, i.Name)
	confP := filepath.Join(p, "conf.cue")
	// if the file does not exists, oldConf will be nil
	oldConf, _ := os.ReadFile(confP)

	newConf, err := json.Marshal(i)
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot marshal config %s", string(newConf))
	}
	if bytes.Compare(oldConf,newConf) == 0 {
		return false, nil
	}

	err = os.RemoveAll(p)
	if err != nil {
		return false, RemoveImageError.Wrap(err, "cannot remove image %s", i.Name)
	}

	err = image.Fetch(from, ver, p)
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot create fetch image %s", i.Name)
	}

	for path, file := range i.Files {
		err = image.Extra(p, path, file.Content, fs.FileMode(file.Permissions))
		if err != nil {
			return false, CreateImageError.Wrap(err, "cannot add file %s to image %s", path, i.Name)
		}
	}

	meta, err := image.ReadMetadata(p)
	if err != nil {
		return false, err
	}
	env := meta.Env
	for k,v := range i.Env {
		env = append(env, strings.Join([]string{k,v}, "="))
	}
	envS := strings.Join(env, "\n")

	err = os.WriteFile(filepath.Join(p, "environment"), []byte(envS), 0666)
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot add environment file to image %s", i.Name)
	}

	err = os.WriteFile(confP, newConf, 0666)
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot create conf for image %s", i.Name)
	}

	return true, nil
}

const unitTemplateStr = `
[Unit]
Description= Sloop service {{.Name}}

[Service]
PrivateNetwork = true
PrivateTmp = true
PrivateDevices = true
PrivateIPC = true
#PrivateUsers = true
ProtectHostname = true
ProtectProc = invisible
MountAPIVFS = true
BindReadOnlyPaths=/dev/log /run/systemd/journal/socket /run/systemd/journal/stdout

RootDirectory = {{.ImageDir}}/rootfs
{{ range $k, $v := .Binds}}
BindPaths = {{$k}}:{{$v}}
{{end}}
EnvironmentFile = {{.ImageDir}}/environment
ExecStart = {{.Cmd}}

[Install]
WantedBy=default.target
`

var unitTemplate *template.Template = template.Must(template.New("unit").Funcs(template.FuncMap{}).Parse(unitTemplateStr))

type UnitConf struct {
	Name string
	ImageDir string
	Binds map[string]string
	Cmd string
}

func handleService(systemd *dbus.Conn, s cue.Service) (bool, error) {
	imageDir := filepath.Join(common.ImagePath, s.Image) 
	bindsMap := make(map[string]string)
	for _,v := range s.Volumes {
		var n string
		if v.Name[0] != '/' {
			n = filepath.Join(common.VolumePath, v.Name)
		} else {
			n = v.Name
		}
		bindsMap[n] = v.Dest
	}
	cmdVec := s.Cmd
	if len(cmdVec) == 0 {
		meta, err := image.ReadMetadata(filepath.Join(common.ImagePath, s.Image))
		if err != nil {
			return false, CreateServiceError.Wrap(err, "failed to get metadata for image %s for service %s", s.Image, s.Name)
		}
		cmdVec = meta.Args
	}
	cmdStr := ""
	for _,c := range cmdVec {
		cmdStr += fmt.Sprintf("%q ", c)
	}
	var buf bytes.Buffer
	conf := UnitConf {
		Name: s.Name,
		ImageDir: imageDir,
		Binds: bindsMap,
		Cmd: cmdStr,
	}
	err := unitTemplate.Execute(&buf, conf)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for service %s", s.Name)
	}
	unitStr := buf.String()

	unitP := filepath.Join(common.UnitPath, s.Name + ".service")

	// If the file is not there, oldUnit will be null
	oldUnit, _ := os.ReadFile(unitP)
	if unitStr == string(oldUnit) {
		return false, nil
	}

	if len(oldUnit) != 0 {
		err = stopDisableDeleteUnit(systemd, s.Name + ".service")
		if err != nil {
			return false, err
		}
	}

	err = os.WriteFile(unitP, []byte(unitStr), 0644)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to write unit file for service %s", s.Name)
	}

	//enable service
	_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{unitP}, false, true)
	if err != nil {
		return false, RuntimeServiceError.Wrap(err, "cannot enable unit for service %s", s.Name)
	}
	return true, nil
}

func stopDisableDeleteUnit(systemd *dbus.Conn, name string) error {
	statuses, err := systemd.ListUnitsByNamesContext(context.Background(), []string{name})
	if err != nil {
		return RuntimeServiceError.Wrap(err, "cannot list unit %s", name)
	}
	if statuses[0].ActiveState == "active" {
		// STOP unit
		wait := make(chan string)
		systemd.StopUnitContext(context.Background(), name, "replace", wait)
		fmt.Printf("stopping %s...\n", name)
		res := <- wait
		if res != "done" {
			return RuntimeServiceError.New("cannot stop unit %s", name)
		}
		fmt.Printf("done\n")
	}
	if statuses[0].LoadState != "not-found" {
		// Disable unit
		_, err = systemd.DisableUnitFilesContext(context.Background(), []string{name}, false)
		if err != nil {
			return RuntimeServiceError.Wrap(err, "cannot disable unit %s: %s", name)
		}
	}
	// Remove unit file
	err = os.RemoveAll(filepath.Join(common.UnitPath, name))
	if err != nil {
		return  RemoveUnitError.Wrap(err, "cannot remove unit %s", name) 
	}
	return nil
}

func Create(config cue.Config) error {
	err := os.MkdirAll(common.VolumePath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create volumes directory") 
	}
	os.MkdirAll(common.ImagePath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create images directory") 
	}
	os.MkdirAll(common.UnitPath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create units directory") 
	}

	reload := false

	for _, v := range config.Volumes {
		err := handleVolume(v)
		if err != nil {
			return err
		}
	}

	curImages, err := os.ReadDir(common.ImagePath)
	if err != nil {
		return  RemoveImageError.Wrap(err, "cannot list current images") 
	}
	for _, ci := range curImages {
		if _, exists := config.Images[ci.Name()]; !exists {
			err = os.RemoveAll(filepath.Join(common.ImagePath, ci.Name()))
			if err != nil {
				return  RemoveImageError.Wrap(err, "cannot remove image %s", ci.Name()) 
			}
		}
	}
	for _,i := range config.Images {
		changed, err := handleImage(i)
		if err != nil {
			return err
		}
		if changed {
			reload = true
		}
	}

	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return  RuntimeServiceError.Wrap(err, "cannot connect to systemd dbus") 
	}

	curUnits, err := os.ReadDir(common.UnitPath)
	if err != nil {
		return  RemoveUnitError.Wrap(err, "cannot list current units") 
	}
	for _, cu := range curUnits {
		if _, exists := config.Services[cu.Name()]; !exists {
			err = stopDisableDeleteUnit(systemd, cu.Name())
			if err != nil {
				return err
			}
			reload = true
		}
	}
	for _, s := range config.Services {
		changed, err := handleService(systemd, s)
		if err != nil {
			return err
		}
		if changed {
			reload = true
		}
	}

	if reload {
		systemd.ReloadContext(context.Background())
	}

	for _, s := range config.Services {
		//start service
		wait := make(chan string)
		systemd.StartUnitContext(context.Background(), s.Name + ".service", "replace", wait)
		fmt.Printf("starting %s...\n", s.Name)
		res := <- wait
		if res != "done" {
			return RuntimeServiceError.New("cannot start service %s", s.Name)
		}
		fmt.Printf("done\n")
	}

	return nil
}

func Purge() error {
	err := os.RemoveAll(common.ImagePath)
	if err != nil {
		return RemoveImageError.Wrap(err, "cannot remove image directory")
	}

	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return  RuntimeServiceError.Wrap(err, "cannot connect to systemd dbus") 
	}

	curUnits, err := os.ReadDir(common.UnitPath)
	if err != nil {
		return  RemoveUnitError.Wrap(err, "cannot list current units")
	}
	for _, cu := range curUnits {
		err = stopDisableDeleteUnit(systemd, cu.Name())
		if err != nil {
			return err
		}
	}

	err = os.RemoveAll(common.UnitPath)
	if err != nil {
		return RemoveUnitError.Wrap(err, "cannot remove unit directory")
	}

	systemd.ReloadContext(context.Background())

	return nil
}
