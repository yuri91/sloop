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
	"github.com/opencontainers/runtime-spec/specs-go"
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
	p := filepath.Join(common.UtilsPath, "catatonit")
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

const nsenterStr string = `#!/bin/bash
service=$1
shift
cmd="$@"
pid=$(head -n 1 /sys/fs/cgroup/sloop.slice/sloop-service-${service}.service/payload/cgroup.procs)
env=$(cat /proc/${pid}/environ | xargs -0)
exec nsenter -a -t ${pid} env -i - ${env} ${cmd}
`
func handleNsenter() error {
	p := filepath.Join(common.UtilsPath, "nsenter")
	oldInit, _ := os.ReadFile(p)
	if bytes.Equal(oldInit, []byte(nsenterStr)) {
		return nil
	}
	err := os.WriteFile(p, []byte(nsenterStr), 0777)
	if err != nil {
		return CreateImageError.Wrap(err, "failed to write nsenter script")
	}
	return nil
}

const hostsBaseStr string = `
127.0.0.1	localhost.localdomain	localhost
::1		localhost.localdomain	localhost

`
func handleEtcHosts(hosts map[string]cue.Host) error {
	hostsStr := hostsBaseStr
	for n,h := range hosts {
		for _, i := range h.Interfaces {
			hostsStr += fmt.Sprintf("%s\t%s\n", i.Ip, n)
		}
	}
	p := filepath.Join(common.UtilsPath, "hosts")
	err := os.WriteFile(p, []byte(hostsStr), 0666)
	if err != nil {
		return CreateImageError.Wrap(err, "failed to write /etc/hosts file")
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
Delegate=yes
ExecStart = systemd-nspawn \
	--quiet \
	--volatile=overlay \
	--keep-unit \
	--register=no \
	--bind-ro={{.UtilsPath}}/hosts:/etc/hosts \
	--bind={{.UtilsPath}}/catatonit:/catatonit \
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
	/catatonit -- {{.Start}}

{{- if ne .Reload "" }}
ExecReload = {{.UtilsPath}}/nsenter {{.Name}} {{.Reload}}
{{- end }}

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
{{ range $n := .Interfaces}}
After = sloop-bridge-{{$n.Bridge.Name}}.service
Requires = sloop-bridge-{{$n.Bridge.Name}}.service
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

{{ range $n := .Interfaces}}
ExecStart = nsenter -t 1 -n -- ip link add {{$.Name}}-{{$n.Name}} type veth peer {{$n.Name}} netns sloop-{{$.Name}}
ExecStart = nsenter -t 1 -n -- ip link set dev {{$.Name}}-{{$n.Name}} up
ExecStart = nsenter -t 1 -n -- ip link set dev {{$.Name}}-{{$n.Name}} master {{$n.Bridge.Name}}
ExecStart = ip link set {{$n.Name}} up
ExecStart = ip addr add {{$n.Ip}}/{{$n.Bridge.Prefix}} dev {{$n.Name}}
ExecStart = ip route add default via {{$n.Bridge.Ip}}
{{end}}

{{ range $n := .Interfaces}}
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
ExecStart = iptables -t nat -A POSTROUTING -s {{.Ip}}/{{.Prefix}} ! -o {{.Name}} -j MASQUERADE

ExecStop = iptables -t nat -D POSTROUTING -s {{.Ip}}/{{.Prefix}} ! -o {{.Name}} -j MASQUERADE
ExecStop = ip link delete {{.Name}}

[Install]
WantedBy=sloop.target
`

const timerTemplateStr = `
[Unit]
Description = Sloop timer {{.Name}}
PartOf = sloop.target

[Timer]
{{- range $cal := .OnCalendar }}
OnCalendar = {{$cal}}
{{- end }}
{{- range $act := .OnActiveSec }}
OnActiveSec = {{$act}}
{{- end }}
Persistent = {{.Persistent}}

[Install]
WantedBy=sloop.target
`

const timerServiceTemplateStr = `
[Unit]
Description = Sloop timer unit {{.Name}}

[Service]
Type = oneshot
{{- range $r := .Run }}
{{- if eq $r.Action "start" }}
ExecStart = systemctl start {{$r.Service}}
{{- else if eq $r.Action "reload" }}
ExecStart = systemctl reload {{$r.Service}}
{{- end}}
{{- end }}

[Install]
WantedBy=sloop.target
`

func capStringLen(length int, source string) string {
	fmt.Printf("%s %d %d\n", source, len(source), length)
	if len(source) <= length {
		return source
	}
	prefix := source[0:(length-4)]
	sha := sha256.Sum256([]byte(source))
	b64 := base64.StdEncoding.EncodeToString(sha[:])
	if len(b64) < 4 {
		for i := 0; i < 4-len(b64); i++ {
			b64 += "1"
		}
	}
	fmt.Printf("%s\n", prefix + b64[0:4])
	return prefix + b64[0:4]
}

var unitTemplate *template.Template = template.Must(template.New("unit").Funcs(template.FuncMap{}).Parse(unitTemplateStr))
var hostTemplate *template.Template = template.Must(template.New("host").Funcs(template.FuncMap{"capStringLen":capStringLen}).Parse(hostTemplateStr))
var bridgeTemplate *template.Template = template.Must(template.New("bridge").Funcs(template.FuncMap{}).Parse(bridgeTemplateStr))
var timerTemplate *template.Template = template.Must(template.New("timer").Funcs(template.FuncMap{}).Parse(timerTemplateStr))
var timerServiceTemplate *template.Template = template.Must(template.New("timerService").Funcs(template.FuncMap{}).Parse(timerServiceTemplateStr))

type UnitConf struct {
	Name string
	UtilsPath string
	BundleDir string
	ServiceDir string
	Binds map[string]string
	Capabilities string
	Start string
	Reload string
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

	changed, err := writeLinkUnit(systemd, unitName, unitStr, false)
	if err != nil {
		return false, err
	}
	return changed, nil
}

func handleHost(systemd *dbus.Conn, h cue.Host) (bool, error) {
	var buf bytes.Buffer
	err := hostTemplate.Execute(&buf, h)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for host %s", h.Name)
	}
	unitStr := buf.String()

	unitName := "sloop-host-" + h.Name + ".service"

	changed, err := writeLinkUnit(systemd, unitName, unitStr, false)
	if err != nil {
		return false, err
	}
	return changed, nil
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

	err = stopUnit(systemd, "sloop-service-" + s.Name + ".service")
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

	for path, file := range s.Image.Files {
		fullP := filepath.Join(p, "files", path)
		if err := os.MkdirAll(filepath.Dir(fullP), 0777); err != nil {
			return false, err
		}
		err := os.WriteFile(fullP, []byte(file.Content), fs.FileMode(file.Permissions))
		if err != nil {
			return false, CreateServiceError.Wrap(err, "cannot add file %s to service %s", path, s.Name)
		}
	}

	meta, err := image.ReadMetadata(getImagePath(s.Image.From))
	if err != nil {
		return false, err
	}
	for k,v := range s.Image.Env {
		meta.Process.Env = append(meta.Process.Env, strings.Join([]string{k,v}, "="))
	}
	if s.Type == "notify" {
		meta.Process.Env = append(meta.Process.Env, "NOTIFY_SOCKET=/run/systemd/notify")
	}
	if s.Host == "" {
		meta.Linux.Namespaces = lo.Filter(meta.Linux.Namespaces, func(n specs.LinuxNamespace, i int) bool {
			return n.Type != "network"
		})
	}
	meta.Process.Capabilities.Bounding = append(meta.Process.Capabilities.Bounding, "CAP_CHOWN")
	meta.Root.Path = getImageRootPath(s.Image.From)

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

func handleService(systemd *dbus.Conn, s cue.Service) (bool, error) {
	serviceDir := filepath.Join(common.ServicePath, s.Name)
	bindsMap := make(map[string]string)
	for _,v := range s.Image.Volumes {
		var n string
		if v.Name[0] != '/' {
			n = filepath.Join(common.VolumePath, v.Name)
		} else {
			n = v.Name
		}
		bindsMap[n] = v.Dest
	}
	for path := range s.Image.Files {
		fullP := filepath.Join(common.ServicePath, s.Name, "files", path)
		bindsMap[fullP] = path
	}

	startVec := s.Exec.Start
	if len(startVec) == 0 {
		meta, err := image.ReadMetadata(getImagePath(s.Image.From))
		if err != nil {
			return false, CreateServiceError.Wrap(err, "failed to get metadata for image %s for service %s", s.Image.From, s.Name)
		}
		startVec = meta.Process.Args
	}
	startStr := ""
	for _,c := range startVec {
		startStr += fmt.Sprintf("%q ", c)
	}
	reloadStr := ""
	for _,c := range s.Exec.Reload {
		reloadStr += fmt.Sprintf("%q ", c)
	}
	if s.Host != "" {
		s.Requires = append(s.Requires, "sloop-host-" + s.Host + ".service")
		s.After = append(s.After, "sloop-host-" + s.Host + ".service")
	}
	var buf bytes.Buffer
	conf := UnitConf {
		Name: s.Name,
		UtilsPath: common.UtilsPath,
		BundleDir: serviceDir,
		ServiceDir: filepath.Join(common.ServicePath, s.Name),
		Binds: bindsMap,
		Capabilities: strings.Join(s.Capabilities, ","),
		Start: startStr,
		Reload: reloadStr,
		Host: s.Host,
		Type: s.Type,
		Enable: s.Enable,
		Wants: s.Wants,
		Requires: s.Requires,
		After: s.After,
	}
	err := unitTemplate.Execute(&buf, conf)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for service %s", s.Name)
	}
	unitStr := buf.String()

	changed, err := writeLinkUnit(systemd, "sloop-service-"+s.Name+".service", unitStr, s.Enable)
	if err != nil {
		return false, err;
	}

	return changed, nil
}

func handleTimer(systemd *dbus.Conn, t cue.Timer) (bool, error) {
	var buf bytes.Buffer
	err := timerTemplate.Execute(&buf, t)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for timer %s", t.Name)
	}
	timerStr := buf.String()
	err = timerServiceTemplate.Execute(&buf, t)
	if err != nil {
		return false, CreateServiceError.Wrap(err, "failed to execute template for timer service %s", t.Name)
	}
	timerServiceStr := buf.String()

	timerP := filepath.Join(common.UnitPath, "sloop-timer-" + t.Name + ".timer")
	serviceP := filepath.Join(common.UnitPath, "sloop-timer-" + t.Name + ".service")

	oldTimer, _ := os.ReadFile(timerP)
	oldService, _ := os.ReadFile(serviceP)
	if timerServiceStr == string(oldTimer) && timerStr == string(oldService) {
		return false, nil
	}

	timerChanged, err := writeLinkUnit(systemd, "sloop-timer-"+t.Name+".timer", timerStr, true)
	if err != nil {
		return false, err;
	}
	unitChanged, err := writeLinkUnit(systemd, "sloop-timer-"+t.Name+".service", timerServiceStr, false)
	if err != nil {
		return false, err;
	}

	return (timerChanged || unitChanged), nil
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
	changed, err := writeLinkUnit(systemd, "sloop.slice", sliceStr, false)
	if err != nil {
		return false, err
	}
	return changed, nil
}

const targetStr string = `
[Unit]
Description=Sloop target
Before=multi-user.target

[Install]
WantedBy=multi-user.target
`

func handleTarget(systemd *dbus.Conn) (bool, error) {
	changed, err := writeLinkUnit(systemd, "sloop.target", targetStr, true)
	if err != nil {
		return false, err
	}
	return changed, nil
}


func startUnit(systemd *dbus.Conn, service string) error {
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
func writeLinkUnit(systemd* dbus.Conn, name string, content string, enable bool) (bool, error) {
	unitP := filepath.Join(common.UnitPath, name)
	oldContent, _ := os.ReadFile(unitP)
	changed := content != string(oldContent)

	err := os.WriteFile(unitP, []byte(content), 0644)
	if err != nil {
		return changed, CreateServiceError.Wrap(err, "failed to write unit %s", name)
	}
	if enable {
		fmt.Printf("Enabling %s...\n", name)
		_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{unitP}, false, true)
	} else {
		fmt.Printf("Linking %s...\n", name)
		_, err = systemd.LinkUnitFilesContext(context.Background(), []string{unitP}, false, true)
	}
	if err != nil {
		return changed, RuntimeServiceError.Wrap(err, "cannot enable unit %s", name)
	}
	return changed, nil
}

func stopUnit(systemd *dbus.Conn, name string) error {
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
	return nil
}
func stopDisableDeleteUnit(systemd *dbus.Conn, name string) error {
	fmt.Printf("Stopping and disabling %s...\n", name)
	statuses, err := systemd.ListUnitsByNamesContext(context.Background(), []string{name})
	if err != nil {
		return RuntimeServiceError.Wrap(err, "cannot list unit %s", name)
	}
	if statuses[0].ActiveState == "active" {
		// STOP unit
		wait := make(chan string)
		systemd.StopUnitContext(context.Background(), name, "replace", wait)
		fmt.Printf("\tstopping %s...\n", name)
		res := <- wait
		if res != "done" {
			return RuntimeServiceError.New("cannot stop unit %s", name)
		}
		fmt.Printf("\t\tdone\n")
	}
	if statuses[0].LoadState != "not-found" {
		// Disable unit
		fmt.Printf("\tdisabling %s...\n", name)
		_, err = systemd.DisableUnitFilesContext(context.Background(), []string{name}, false)
		if err != nil {
			return RuntimeServiceError.Wrap(err, "cannot disable unit %s: %v", name, statuses[0])
		}
		fmt.Printf("\t\tdone\n")
	}
	// Remove unit file
	err = os.RemoveAll(filepath.Join(common.UnitPath, name))
	if err != nil {
		return  RemoveUnitError.Wrap(err, "cannot remove unit %s", name) 
	}
	return nil
}


func gatherImages(services map[string]cue.Service) []string {
	imgMap := lo.MapEntries(services, func(n string, s cue.Service) (string, bool) {
		return s.Image.From, true
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
func getCurTimers() ([]string, error) {
	return getCur("timer")
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
	os.MkdirAll(common.UtilsPath, 0700)
	if err != nil {
		return  FilesystemError.Wrap(err, "cannot create utils directory") 
	}

	reload := false

	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return  RuntimeServiceError.Wrap(err, "cannot connect to systemd dbus") 
	}

	configUnits := []string{"sloop.target", "sloop.slice"}
	configUnits = append(configUnits, lo.Map(lo.Keys(config.Services), func (s string, _ int) string {
		return "sloop-service-"+s+".service"
	})...)
	configUnits = append(configUnits, lo.Map(lo.Keys(config.Timers), func (s string, _ int) string {
		return "sloop-timer-"+s+".timer"
	})...)
	configUnits = append(configUnits, lo.Map(lo.Keys(config.Timers), func (s string, _ int) string {
		return "sloop-timer-"+s+".service"
	})...)
	configUnits = append(configUnits, lo.Map(lo.Keys(config.Bridges), func (s string, _ int) string {
		return "sloop-bridge-"+s+".service"
	})...)
	configUnits = append(configUnits, lo.Map(lo.Keys(config.Hosts), func (s string, _ int) string {
		return "sloop-host-"+s+".service"
	})...)
	curImages, err := getCurImages();
	if err != nil {
		return err
	}

	curUnits, err := getCurUnits()
	if err != nil {
		return err
	}
	unitsToRemove, _ := lo.Difference(curUnits, configUnits)
	for _, u := range unitsToRemove {
		err = stopDisableDeleteUnit(systemd, u)
		if err != nil {
			return err
		}
		reload = true
	}

	err = handleInit()
	if err != nil {
		return err
	}

	err = handleNsenter()
	if err != nil {
		return err
	}

	err = handleEtcHosts(config.Hosts)
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


	for n, b := range config.Bridges {
		changed, err := handleBridge(systemd, b)
		if err != nil {
			return err
		}
		if changed {
			err = stopUnit(systemd, "sloop-bridge-"+n+".service")
			if err != nil {
				return err
			}
			reload = true
		}
	}

	for n, h := range config.Hosts {
		changed, err := handleHost(systemd, h)
		if err != nil {
			return err
		}
		if changed {
			err = stopUnit(systemd, "sloop-host-"+n+".service")
			if err != nil {
				return err
			}
			reload = true
		}
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

	for n, s := range config.Services {
		changed, err := handleServiceFiles(systemd, s)
		if err != nil {
			return err
		}
		changed2, err := handleService(systemd, s)
		if err != nil {
			return err
		}
		if changed2 && !changed {
			err = stopUnit(systemd, "sloop-service-"+n+".service")
			if err != nil {
				return err
			}
		}
		reload = reload || changed || changed2
	}

	for n, t := range config.Timers {
		changed, err := handleTimer(systemd, t)
		if err != nil {
			return err
		}
		if changed {
			err = stopUnit(systemd, "sloop-timer-"+n+".timer")
			if err != nil {
				return err
			}
			err = stopUnit(systemd, "sloop-timer-"+n+".service")
			if err != nil {
				return err
			}
			reload = true
		}
	}

	if reload {
		systemd.ReloadContext(context.Background())
	}

	err = startUnit(systemd, "sloop.target")
	if err != nil {
		return err
	}

	return nil
}

func Purge(images bool) error {
	if images {
		err := os.RemoveAll(common.ImagePath)
		if err != nil {
			return RemoveImageError.Wrap(err, "cannot remove image directory")
		}
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
