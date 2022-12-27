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
	"yuri91/sloop/catatonit"
	"yuri91/sloop/common"
	"yuri91/sloop/cue"
	"yuri91/sloop/image"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/samber/lo"
)

func replaceLast(orig, old, new_ string) string {
	i := strings.LastIndex(orig, old)
	if i == -1 {
		return orig
	}
	return orig[:i] + new_ + orig[i+len(old):]
}
func getImagePath(from string) string {
	from = replaceLast(from, ":", "-")
	path := filepath.Join(common.ImagePath, from)
	return path
}
func getImageRootPath(from string) string {
	path := filepath.Join(getImagePath(from), "rootfs")
	return path
}

func handleInit() error {
	p := filepath.Join(common.ImagePath, "catatonit")
	oldInit, _ := os.ReadFile(p)
	if bytes.Equal(oldInit, catatonit.Bin) {
		return nil
	}
	err := os.WriteFile(p, catatonit.Bin, 0777)
	if err != nil {
		return CreateImageError.Wrap(err, "failed to write catatonit binary")
	}
	return nil
}

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

func handleImage(img string) error {
	parts := strings.SplitN(img, ":", 2)
	from := parts[0]
	ver := parts[1]

	p := getImagePath(img)

	err := image.Fetch(from, ver, p)
	if err != nil {
		return CreateImageError.Wrap(err, "cannot fetch image %s", img)
	}

	return nil
}
const unitTemplateStr = `
[Unit]
Description= Sloop service {{.Name}}
PartOf = sloop.target
Before = sloop.target
{{ range $u := .Wants}}
Wants = {{$u}}
{{end}}
{{ range $u := .Requires}}
Requires = {{$u}}
{{end}}
{{ range $u := .After}}
After = {{$u}}
{{end}}

[Service]
Slice=sloop.slice
{{- if eq .Type "oneshot" }}
Type = oneshot
{{- else }}
Type = notify
{{- end }}
NotifyAccess=all
{{- if ne .Type "oneshot" }}
RestartForceExitStatus=133
SuccessExitStatus=133
{{- end }}
KillMode=mixed
ExecStart = systemd-nspawn \
	--quiet \
	--volatile=overlay \
	--keep-unit \
	--register=no \
	--bind={{.InitPath}}:/catatonit \
	--kill-signal=SIGTERM \
	--oci-bundle={{.BundleDir}} \
	-M {{.Name}} \
	--resolv-conf=bind-uplink \
{{- if ne .Host "" }}
	--network-namespace-path=/var/run/netns/sloop-{{.Host}} \
{{- end }}
{{- range $k, $v := .Binds }}
	--bind={{$k}}:{{$v}} \
{{- end }}
{{- if eq .Type "notify" }}
	--bind=/run/systemd/notify \
{{- end }}
{{- if ne .Capabilities "" }}
	--capability={{.Capabilities}} \
{{- end }}
	/catatonit -- {{.Cmd}}

{{- if eq .Type "notify" }}
Environment=NOTIFY_SOCKET=
{{- end }}

{{- if .Enable }}
[Install]
WantedBy=sloop.target
{{- end }}
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
Slice=sloop.slice
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
WantedBy=sloop.target
`

const bridgeTemplateStr = `
[Unit]
Description = Sloop bridge {{.Name}}
After = network-online.target
StopWhenUnneeded = yes

[Service]
Slice=sloop.slice
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
WantedBy=sloop.target
`

var unitTemplate *template.Template = template.Must(template.New("unit").Funcs(template.FuncMap{}).Parse(unitTemplateStr))
var hostTemplate *template.Template = template.Must(template.New("host").Funcs(template.FuncMap{}).Parse(hostTemplateStr))
var bridgeTemplate *template.Template = template.Must(template.New("bridge").Funcs(template.FuncMap{}).Parse(bridgeTemplateStr))

type UnitConf struct {
	Name string
	InitPath string
	BundleDir string
	ServiceDir string
	Binds map[string]string
	Capabilities string
	Cmd string
	Host string
	Type string
	Enable bool
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
	unitP := filepath.Join(common.UnitPath, unitName)

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

	//link service
	_, err = systemd.LinkUnitFilesContext(context.Background(), []string{unitP}, false, true)
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
	unitP := filepath.Join(common.UnitPath, unitName)

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
	_, err = systemd.LinkUnitFilesContext(context.Background(), []string{unitP}, false, true)
	if err != nil {
		return false, RuntimeServiceError.Wrap(err, "cannot enable unit for host %s", h.Name)
	}
	return true, nil
}

func handleServiceFiles(systemd *dbus.Conn, s cue.Service) (bool, error) {

	p := filepath.Join(common.ServicePath, s.Name)
	confP := filepath.Join(p, "conf.cue")
	// if the file does not exists, oldConf will be nil
	oldConf, _ := os.ReadFile(confP)

	newConf, err := json.MarshalIndent(s, "", "\t")
	if err != nil {
		return false, CreateServiceError.Wrap(err, "cannot marshal config %s", string(newConf))
	}
	if bytes.Compare(oldConf,newConf) == 0 {
		return false, nil
	}

	err = stopDisableDeleteUnit(systemd, "sloop-service-" + s.Name + ".service")
	if err != nil {
		return false, err
	}

	err = os.RemoveAll(p)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "cannot remove service %s files", s.Name)
	}

	err = os.MkdirAll(filepath.Join(p, "files"), 700)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "cannot create service %s directory", s.Name)
	}

	for path, file := range s.Files {
		fullP := filepath.Join(p, "files", path)
		if err := os.MkdirAll(filepath.Dir(fullP), 0777); err != nil {
			return false, err
		}
		err := os.WriteFile(fullP, []byte(file.Content), fs.FileMode(file.Permissions))
		if err != nil {
			return false, CreateServiceError.Wrap(err, "cannot add file %s to service %s", path, s.Name)
		}
	}

	meta, err := image.ReadMetadata(getImagePath(s.From))
	if err != nil {
		return false, err
	}
	for k,v := range s.Env {
		meta.Process.Env = append(meta.Process.Env, strings.Join([]string{k,v}, "="))
	}
	if s.Type == "notify" {
		meta.Process.Env = append(meta.Process.Env, "NOTIFY_SOCKET=/run/systemd/notify")
	}
	meta.Process.Capabilities.Bounding = append(meta.Process.Capabilities.Bounding, "CAP_CHOWN")
	meta.Root.Path = getImageRootPath(s.From)

	metaB, err := json.MarshalIndent(meta, "", "\t")
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot marshal OCI config for service %s", s.Name)
	}
	err = os.WriteFile(filepath.Join(p, "config.json"), metaB, 0666)
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot add OCI config file to service %s", s.Name)
	}

	err = os.WriteFile(confP, newConf, 0666)
	if err != nil {
		return false, CreateImageError.Wrap(err, "cannot create conf for service %s", s.Name)
	}

	return true, nil
}

func handleService(systemd *dbus.Conn, s cue.Service) error {
	serviceDir := filepath.Join(common.ServicePath, s.Name)
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
	for path := range s.Files {
		fullP := filepath.Join(common.ServicePath, s.Name, "files", path)
		bindsMap[fullP] = path
	}

	cmdVec := s.Cmd
	if len(cmdVec) == 0 {
		meta, err := image.ReadMetadata(getImagePath(s.From))
		if err != nil {
			return CreateServiceError.Wrap(err, "failed to get metadata for image %s for service %s", s.From, s.Name)
		}
		cmdVec = meta.Process.Args
	}
	cmdStr := ""
	for _,c := range cmdVec {
		cmdStr += fmt.Sprintf("%q ", c)
	}
	if s.Host != "" {
		s.Requires = append(s.Requires, "sloop-host-" + s.Host + ".service")
		s.After = append(s.After, "sloop-host-" + s.Host + ".service")
	}
	var buf bytes.Buffer
	conf := UnitConf {
		Name: s.Name,
		InitPath: filepath.Join(common.ImagePath, "catatonit"),
		BundleDir: serviceDir,
		ServiceDir: filepath.Join(common.ServicePath, s.Name),
		Binds: bindsMap,
		Capabilities: strings.Join(s.Capabilities, ","),
		Cmd: cmdStr,
		Host: s.Host,
		Type: s.Type,
		Enable: s.Enable,
		Wants: s.Wants,
		Requires: s.Requires,
		After: s.After,
	}
	err := unitTemplate.Execute(&buf, conf)
	if err != nil {
		return CreateServiceError.Wrap(err, "failed to execute template for service %s", s.Name)
	}
	unitStr := buf.String()

	unitP := filepath.Join(common.UnitPath, "sloop-service-" + s.Name + ".service")

	err = os.WriteFile(unitP, []byte(unitStr), 0644)
	if err != nil {
		return CreateServiceError.Wrap(err, "failed to write unit file for service %s", s.Name)
	}

	//enable service
	if s.Enable {
		_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{unitP}, false, true)
	} else {
		_, err = systemd.LinkUnitFilesContext(context.Background(), []string{unitP}, false, true)
	}
	if err != nil {
		return RuntimeServiceError.Wrap(err, "cannot enable unit for service %s", s.Name)
	}
	return nil
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

const sliceStr string = `
[Unit]
Description=Slice used to run sloop services
Before=slices.target

[Slice]
MemoryAccounting=true
IOAccounting=true
CPUAccounting=true
`

func handleSlice(systemd *dbus.Conn) (bool, error) {
	unitP := filepath.Join(common.UnitPath, "sloop.slice")

	oldUnit, _ := os.ReadFile(unitP)
	if targetStr == string(oldUnit) {
		return false, nil
	}

	if oldUnit != nil {
		err := stopDisableDeleteUnit(systemd, "sloop.slice")
		if err != nil {
			return false, err
		}
	}

	err := os.WriteFile(unitP, []byte(sliceStr), 0644)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to write unit file for  sloop.slice")
	}
	_, err = systemd.LinkUnitFilesContext(context.Background(), []string{unitP}, false, true)
	if err != nil {
		return false, RuntimeServiceError.Wrap(err, "cannot link sloop.slice")
	}
	return true, nil
}

const targetStr string = `
[Unit]
Description=Sloop target
Before=multi-user.target

[Install]
WantedBy=multi-user.target
`

func handleTarget(systemd *dbus.Conn) (bool, error) {
	unitP := filepath.Join(common.UnitPath, "sloop.target")

	oldUnit, _ := os.ReadFile(unitP)
	if targetStr == string(oldUnit) {
		return false, nil
	}

	err := stopDisableDeleteUnit(systemd, "sloop.target")
	if err != nil {
		return false, err
	}

	err = os.WriteFile(unitP, []byte(targetStr), 0644)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to write unit file for  sloop.target")
	}
	_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{unitP}, false, true)
	if err != nil {
		return false, RuntimeServiceError.Wrap(err, "cannot enable sloop.target")
	}
	return true, nil
}

func startService(systemd *dbus.Conn, service string) error {
	wait := make(chan string)
	systemd.StartUnitContext(context.Background(), service, "replace", wait)
	fmt.Printf("starting %s...\n", service)
	res := <- wait
	if res != "done" {
		return RuntimeServiceError.New("cannot start service %s", service)
	}
	fmt.Printf("done\n")
	return nil
}


func gatherImages(services map[string]cue.Service) []string {
	imgMap := lo.MapEntries(services, func(n string, s cue.Service) (string, bool) {
		return s.From, true
	})
	return lo.Keys(imgMap)
}

func getCurImages() ([]string, error) {
	var curImages []string
	err := filepath.WalkDir(common.ImagePath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if _, err = os.Stat(filepath.Join(path, "umoci.json")); err != nil {
			return nil
		}
		name := strings.TrimPrefix(path, common.ImagePath + "/")
		name = replaceLast(name, "-", ":")
		curImages = append(curImages, name)
		return filepath.SkipDir
	})
	if err != nil {
		return  nil, RemoveImageError.Wrap(err, "cannot list current images") 
	}
	return curImages, nil
}

func getCurUnits() ([]string, error) {
	curElems, err := os.ReadDir(common.UnitPath)
	if err != nil {
		return  nil, RemoveUnitError.Wrap(err, "cannot list current units")
	}
	curUnits := lo.Map(curElems, func(e os.DirEntry, i int) string {
		return e.Name()
	})
	return curUnits, nil
}

func getCur(kind string) ([]string, error) {
	curUnits, err := getCurUnits()
	if err != nil {
		return  nil, err
	}
	curKind := lo.FilterMap(curUnits, func(e string, i int) (string, bool) {
		if !strings.HasPrefix(e, "sloop-" + kind + "-") {
			return "", false
		}
		trimmed := strings.TrimPrefix(e, "sloop-" + kind + "-")
		return strings.TrimSuffix(trimmed, ".service"), true
	})
	return curKind, nil
}

func getCurServices() ([]string, error) {
	return getCur("service")
}

func getCurBridges() ([]string, error) {
	return getCur("bridge")
}

func getCurHosts() ([]string, error) {
	return getCur("host")
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
	os.MkdirAll(common.ServicePath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create services directory") 
	}

	reload := false

	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return  RuntimeServiceError.Wrap(err, "cannot connect to systemd dbus") 
	}

	curImages, err := getCurImages();
	if err != nil {
		return err
	}
	curServices, err := getCurServices()
	if err != nil {
		return err
	}
	curBridges, err := getCurBridges()
	if err != nil {
		return err
	}
	curHosts, err := getCurHosts()
	if err != nil {
		return err
	}

	err = handleInit()
	if err != nil {
		return err
	}

	changed, err := handleSlice(systemd)
	if err != nil {
		return err
	}
	if changed {
		reload = true
	}

	changed, err = handleTarget(systemd)
	if err != nil {
		return err
	}
	if changed {
		reload = true
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
	bridgesToRemove, _ := lo.Difference(curBridges, lo.Keys(config.Bridges))
	for _, b := range bridgesToRemove {
		err = stopDisableDeleteUnit(systemd, "sloop-bridge-"+b+".service")
		if err != nil {
			return err
		}
		reload = true
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
	hostsToRemove, _ := lo.Difference(curHosts, lo.Keys(config.Hosts))
	for _, b := range hostsToRemove {
		err = stopDisableDeleteUnit(systemd, "sloop-host-"+b+".service")
		if err != nil {
			return err
		}
		reload = true
	}

	for _, v := range config.Volumes {
		err := handleVolume(v)
		if err != nil {
			return err
		}
	}

	images := gatherImages(config.Services)
	imagesToRemove, imagesToAdd := lo.Difference(curImages, images)
	for _,i := range imagesToAdd {
		err := handleImage(i)
		if err != nil {
			return err
		}
	}
	for _, ci := range imagesToRemove {
		err = os.RemoveAll(filepath.Join(common.ImagePath, ci))
		if err != nil {
			return  RemoveImageError.Wrap(err, "cannot remove image %s", ci) 
		}
	}

	servicesToRemove, _ := lo.Difference(curServices, lo.Keys(config.Services))
	for _, cu := range servicesToRemove {
		err = stopDisableDeleteUnit(systemd, cu+".service")
		if err != nil {
			return err
		}
		reload = true
	}
	for _, s := range config.Services {
		changed, err := handleServiceFiles(systemd, s)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		err = handleService(systemd, s)
		if err != nil {
			return err
		}
		reload = true
	}

	if reload {
		systemd.ReloadContext(context.Background())
	}

	for _, s := range config.Services {
		if !s.Enable {
			continue
		}
		//start service
		err = startService(systemd, "sloop-service-" + s.Name + ".service")
		if err != nil {
			return err
		}
	}
	err = startService(systemd, "sloop.target")
	if err != nil {
		return err
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

	err = os.RemoveAll(common.ServicePath)
	if err != nil {
		return RemoveUnitError.Wrap(err, "cannot remove service directory")
	}

	systemd.ReloadContext(context.Background())

	return nil
}
