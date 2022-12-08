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
{{ range $u := .Requires}}
Requires = {{$u}}
{{end}}
{{ range $u := .Wants}}
Wants = {{$u}}
{{end}}
{{ range $u := .Requires}}
Requires = {{$u}}
{{end}}
{{ range $u := .After}}
After = {{$u}}
{{end}}
{{ if ne .Host "" }}
JoinsNamespaceOf = sloop-host-{{.Host}}.service
{{end}}

[Service]
{{ if ne .Host "" }}
PrivateNetwork = true
{{end}}
PrivateTmp = true
PrivateDevices = true
PrivateIPC = true
#PrivateUsers = true
ProtectHostname = true
ProtectProc = invisible
MountAPIVFS = true
BindReadOnlyPaths=/dev/log /run/systemd/journal/socket /run/systemd/journal/stdout

RootDirectory = {{.ImageDir}}/rootfs
ReadOnlyPaths = /
{{ range $k, $v := .Binds}}
BindPaths = {{$k}}:{{$v}}
{{end}}
EnvironmentFile = {{.ImageDir}}/environment
ExecStart = {{.Cmd}}

[Install]
WantedBy=default.target
`

const hostTemplateStr = `
[Unit]
Description = Sloop network namespace {{.Name}}
After = network-online.target
StopWhenUnneeded = yes
{{ range $n := .Netdevs}}
After = sloop-bridge-{{$n.Bridge}}.service
Requires = sloop-bridge-{{$n.Bridge}}.service
{{end}}

[Service]
Type = oneshot
RemainAfterExit = true
PrivateNetwork = yes

ExecStart = ip netns add sloop-{{.Name}}
ExecStart = umount /var/run/netns/sloop-{{.Name}}
ExecStart = mount --bind /proc/self/ns/net /var/run/netns/sloop-{{.Name}}

ExecStart = ip link set lo up

{{ range $n := .Netdevs}}
ExecStart = nsenter -t 1 -n -- ip link add {{$n.Bridge}}-{{$.Name}}-{{$n.Name}} type veth peer {{$n.Name}} netns sloop-{{$.Name}}
ExecStart = nsenter -t 1 -n -- ip link set dev {{$n.Bridge}}-{{$.Name}}-{{$n.Name}} up
ExecStart = nsenter -t 1 -n -- ip link set dev {{$n.Bridge}}-{{$.Name}}-{{$n.Name}} master {{$n.Bridge}}
ExecStart = ip link set {{$n.Name}} up
ExecStart = ip addr add {{$n.Ip}}/16 dev {{$n.Name}}
ExecStart = ip route add default via {{$n.BridgeIp}}
{{end}}

{{ range $n := .Netdevs}}
ExecStop = ip link delete {{$n.Name}}
{{end}}

ExecStop = ip netns delete sloop-{{.Name}}

[Install]
WantedBy=default.target
`

const bridgeTemplateStr = `
[Unit]
Description = Sloop bridge {{.Name}}
After = network-online.target
StopWhenUnneeded = yes

[Service]
Type = oneshot
RemainAfterExit = true

ExecStart = sysctl net.ipv4.ip_forward=1
ExecStart = ip link add {{.Name}} type bridge
ExecStart = ip link set {{.Name}} up
ExecStart = ip addr add {{.Ip}}/16 dev {{.Name}}
ExecStart = iptables -t nat -A POSTROUTING -s {{.Ip}}/16 ! -o {{.Name}} -j MASQUERADE

ExecStop = iptables -t nat -D POSTROUTING -s {{.Ip}}/16 ! -o {{.Name}} -j MASQUERADE
ExecStop = ip link delete {{.Name}}

[Install]
WantedBy=default.target
`

var unitTemplate *template.Template = template.Must(template.New("unit").Funcs(template.FuncMap{}).Parse(unitTemplateStr))
var hostTemplate *template.Template = template.Must(template.New("host").Funcs(template.FuncMap{}).Parse(hostTemplateStr))
var bridgeTemplate *template.Template = template.Must(template.New("bridge").Funcs(template.FuncMap{}).Parse(bridgeTemplateStr))

type UnitConf struct {
	Name string
	ImageDir string
	Binds map[string]string
	Cmd string
	Host string
	Wants []string
	Requires []string
	After []string
}
type NetConf struct {
	Bridge string
}

func handleBridge(systemd *dbus.Conn, b cue.Bridge) (bool, error) {
	var buf bytes.Buffer
	err := bridgeTemplate.Execute(&buf, b)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for bridge %s", b.Name)
	}
	unitStr := buf.String()

	unitName := "sloop-bridge-" + b.Name + ".service"
	unitP := filepath.Join(common.BridgePath, unitName)

	// If the file is not there, oldUnit will be null
	oldUnit, _ := os.ReadFile(unitP)
	if unitStr == string(oldUnit) {
		return false, nil
	}

	if len(oldUnit) != 0 {
		err = stopDisableDeleteUnit(systemd, unitName)
		if err != nil {
			return false, err
		}
	}

	err = os.WriteFile(unitP, []byte(unitStr), 0644)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to write unit file for bridge %s", b.Name)
	}

	//enable service
	_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{unitP}, false, true)
	if err != nil {
		return false, RuntimeServiceError.Wrap(err, "cannot enable unit for bridge %s", b.Name)
	}
	return true, nil
}

func handleHost(systemd *dbus.Conn, h cue.Host) (bool, error) {
	var buf bytes.Buffer
	err := hostTemplate.Execute(&buf, h)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for host %s", h.Name)
	}
	unitStr := buf.String()

	unitName := "sloop-host-" + h.Name + ".service"
	unitP := filepath.Join(common.BridgePath, unitName)

	// If the file is not there, oldUnit will be null
	oldUnit, _ := os.ReadFile(unitP)
	if unitStr == string(oldUnit) {
		return false, nil
	}

	if len(oldUnit) != 0 {
		err = stopDisableDeleteUnit(systemd, unitName)
		if err != nil {
			return false, err
		}
	}

	err = os.WriteFile(unitP, []byte(unitStr), 0644)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to write unit file for host %s", h.Name)
	}

	//enable service
	_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{unitP}, false, true)
	if err != nil {
		return false, RuntimeServiceError.Wrap(err, "cannot enable unit for host %s", h.Name)
	}
	return true, nil
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
	s.Requires = append(s.Requires, "sloop-host-" + s.Host + ".service")
	s.After = append(s.After, "sloop-host-" + s.Host + ".service")
	var buf bytes.Buffer
	conf := UnitConf {
		Name: s.Name,
		ImageDir: imageDir,
		Binds: bindsMap,
		Cmd: cmdStr,
		Host: s.Host,
		Wants: s.Wants,
		Requires: s.Requires,
		After: s.After,
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
	os.MkdirAll(common.BridgePath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create bridges directory") 
	}
	os.MkdirAll(common.HostPath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create hosts directory") 
	}

	reload := false

	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return  RuntimeServiceError.Wrap(err, "cannot connect to systemd dbus") 
	}


	for _, b := range config.Bridges {
		changed, err := handleBridge(systemd, b)
		if err != nil {
			return err
		}
		if changed {
			reload = true
		}
	}
	for _, h := range config.Hosts {
		changed, err := handleHost(systemd, h)
		if err != nil {
			return err
		}
		if changed {
			reload = true
		}
	}

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
	if err == nil {
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
	}

	curHosts, err := os.ReadDir(common.HostPath)
	if err == nil {
		for _, cu := range curHosts {
			err = stopDisableDeleteUnit(systemd, cu.Name())
			if err != nil {
				return err
			}
		}
		err = os.RemoveAll(common.HostPath)
		if err != nil {
			return RemoveUnitError.Wrap(err, "cannot remove host directory")
		}
	}

	curBridges, err := os.ReadDir(common.BridgePath)
	if err == nil {
		for _, cu := range curBridges {
			err = stopDisableDeleteUnit(systemd, cu.Name())
			if err != nil {
				return err
			}
		}
		err = os.RemoveAll(common.BridgePath)
		if err != nil {
			return RemoveUnitError.Wrap(err, "cannot remove bridge directory")
		}
	}

	systemd.ReloadContext(context.Background())

	return nil
}
