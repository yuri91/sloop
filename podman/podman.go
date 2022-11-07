package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/containers/buildah/define"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/generate"
	"github.com/containers/podman/v4/pkg/specgen"
	spec "github.com/opencontainers/runtime-spec/specs-go"

	nettypes "github.com/containers/common/libnetwork/types"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/bindings/network"
	"github.com/containers/podman/v4/pkg/bindings/volumes"
	"github.com/containers/podman/v4/pkg/domain/entities"

	"github.com/coreos/go-systemd/v22/dbus"

	"yuri91/sloop/cue"
)

type podmanError struct {
	err error
	msg string
}
func (e podmanError) Error() string {
	return e.msg + ": " + e.err.Error()
}
func wrapPodmanError(err error, msg string) podmanError {
	return podmanError{err, msg}
}

const unitsDir = ".units/"

const containerTemplateStr = `
FROM {{ .From }}
{{ range $p, $f := .Files}}
COPY --chmod={{ Octal $f.Permissions }} {{ ConvPath $p }} {{$p}}
{{end}}
LABEL "sloop"=""
{{ range $k, $v := .Labels}}
LABEL {{$k}}={{$v}}
{{end}}
{{ range $k, $v := .Env}}
LABEL {{$k}}={{$v}}
{{end}}
{{if  .Entrypoint}}
ENTRYPOINT [{{ StringsJoin .Entrypoint ","}}]
{{end}}
{{if .Cmd}}
CMD [{{ StringsJoin .Cmd ","}}]
{{end}}
`

func stringsJoin(strings []string, sep string) string {
	var ret string
	for i, s := range strings {
		if i != 0 {
			ret += sep
		}
		ret += fmt.Sprintf("%q", s)
	}
	return ret
}
func convPath(p string) string {
	return strings.ReplaceAll(p, "/", "_")
}
func octal(i uint16) string {
	return fmt.Sprintf("%o", uint32(i))
}
func buildImage(conn context.Context, i cue.Image, containerTemplate *template.Template) (*entities.BuildReport, error) {
	var buf bytes.Buffer
	err := containerTemplate.Execute(&buf, i)
	if err != nil {
		return nil, wrapPodmanError(err, "Error when executing Containerfile template")
	}
	containerStr := buf.String()
	tmpdir, err := os.MkdirTemp("", "sloop_"+i.Name)
	if err != nil {
		return nil, wrapPodmanError(err, "Error in creating temporary directory")
	}
	defer os.RemoveAll(tmpdir)
	err = os.Chdir(tmpdir)
	os.WriteFile("Containerfile", []byte(containerStr), fs.FileMode(0666))
	if err != nil {
		return nil, wrapPodmanError(err, "Cannot cd to temporary directory")
	}
	for p,f := range i.Files {
		err = os.WriteFile(convPath(p), []byte(f.Content), fs.FileMode(f.Permissions))
		if err != nil {
			return nil, wrapPodmanError(err, "Cannot write file to temporary directory")
		}
	}
	report, err := images.Build(conn, []string{"Containerfile"}, entities.BuildOptions {
		BuildOptions: define.BuildOptions {
			Timestamp: &time.Time{},
			Layers: true,
			Quiet: true,
		},
	})
	if err != nil {
		return report, wrapPodmanError(err, "Cannot build image")
	}
	// TODO build image
	return report, nil
}
func getVolumes(s cue.Service) []*specgen.NamedVolume {
	var ret []*specgen.NamedVolume
	for _, v := range s.Volumes {
		if v.Name[0] == '/' {
			continue
		}
		vol := specgen.NamedVolume {
			Name: v.Name,
			Dest: v.Dest,
		}
		ret = append(ret, &vol)
	}
	return ret
}
func getMounts(s cue.Service) []spec.Mount {
	var ret []spec.Mount
	for _, v := range s.Volumes {
		if v.Name[0] != '/' {
			continue
		}
		mount := spec.Mount {
			Source: v.Name,
			Destination: v.Dest,
			Type: "bind",
		}
		ret = append(ret, mount)
	}
	return ret
}
func getNetworks(s cue.Service) map[string]nettypes.PerNetworkOptions {
	ret := make(map[string]nettypes.PerNetworkOptions, len(s.Networks))
	for _, n := range s.Networks {
		ret[n] = nettypes.PerNetworkOptions{}
	}
	return ret
}
func getPortMappings(s cue.Service) []nettypes.PortMapping {
	ret := make([]nettypes.PortMapping, len(s.Ports))
	for i, p := range s.Ports {
		ret[i].ContainerPort = p.Service
		ret[i].HostPort = p.Host
	}
	return ret
}
func matchSpec(conn context.Context, id string, spec specgen.SpecGenerator) (bool, error) {
	false_ := false
	data, err := containers.Inspect(conn, id, &containers.InspectOptions {
		Size: &false_,
	})
	if err != nil {
		return false, wrapPodmanError(err, "Error while inspecting container")
	}
	origSpecStr := data.Config.Annotations["sloop_config"]
	specBytes, err := json.Marshal(spec)
	if err != nil {
		return false, wrapPodmanError(err, "Error when marshaling spec")
	}
	specStr := string(specBytes)
	return specStr == origSpecStr, nil
}

func createVolumes(conn context.Context, vols map[string]cue.Volume) error {
	for _, v := range vols {
		if v.Name[0] == '/' {
			continue
		}
		list, err := volumes.List(conn, &volumes.ListOptions {
			Filters: map[string][]string{
				"label": {fmt.Sprintf("sloop_volume=%s", v.Name)},
			},
		})
		if err != nil {
			return wrapPodmanError(err, "Error when listing volumes")
		}
		if len(list) > 1 {
			return fmt.Errorf("Too many volumes matching filter")
		}
		if len(list) == 0 {
			_, err = volumes.Create(conn, entities.VolumeCreateOptions {
				Name: v.Name,
				Labels: map[string]string{"sloop_volume": v.Name},
			}, nil)
			if err != nil {
				return wrapPodmanError(err, "Error when creating volume")
			}
		}
	}
	return nil
}

func createNetworks(conn context.Context, nets map[string]cue.Network) error {
	for _, v := range nets {
		list, err := network.List(conn, &network.ListOptions {
			Filters: map[string][]string{
				"label": {fmt.Sprintf("sloop_network=%s", v.Name)},
			},
		})
		if err != nil {
			return wrapPodmanError(err, "Error when listing networks")
		}
		if len(list) > 1 {
			return fmt.Errorf("Too many networks matching filter")
		}
		if len(list) == 0 {
			_, err = network.Create(conn, &nettypes.Network {
				Name: v.Name,
				Labels: map[string]string{"sloop_network": v.Name},
			})
			if err != nil {
				return wrapPodmanError(err, "Error when creating network")
			}
		}
	}
	return nil
}

func createImages(conn context.Context, imgs map[string]cue.Image) (map[string]string, error) {
	containerTemplate := template.Must(template.New("containerfile").Funcs(template.FuncMap{"StringsJoin": stringsJoin, "ConvPath":convPath, "Octal": octal}).Parse(containerTemplateStr))

	builds := make(map[string]string)
	for n, i := range imgs {
		id, err := buildImage(conn, i, containerTemplate)
		if err != nil {
			return nil, err
		}
		builds[n] = id.ID
	}
	return builds, nil
}

func findAllContainers(conn context.Context, services map[string]cue.Service) (map[string]string, error) {
	oldContainers := make(map[string]string)
	for n := range services {
		true_ := true
		list, err := containers.List(conn, &containers.ListOptions {
			All: &true_,
			Filters: map[string][]string{
				"label": {fmt.Sprintf("sloop_service=%s", n)},
			},
		})
		if err != nil {
			return nil, wrapPodmanError(err, "Error when listing existing containers")
		}
		if len(list) > 1 {
			return nil, fmt.Errorf("Multiple existing containers for one name")
		}
		if len(list) == 1 {
			oldContainers[n] = list[0].ID
		}
	}
	return oldContainers, nil
}

func createContainers(conn context.Context, services map[string]cue.Service, images map[string]string, oldContainers map[string]string) (map[string]string, error) {
	newContainers := make(map[string]string)
	for n, s := range services {
		spec := specgen.NewSpecGenerator(images[s.Image], false)
		spec.Volumes = getVolumes(s)
		spec.Mounts = getMounts(s)
		spec.Networks = getNetworks(s)
		spec.PortMappings = getPortMappings(s)
		spec.Labels = map[string]string{"sloop_service": n}
		exists := false
		if oldc, ok := oldContainers[n]; ok {
			var err error
			exists, err = matchSpec(conn, oldc, *spec)
			if err != nil {
				return nil, err
			}
		}
		var id string
		if exists {
			id = oldContainers[n]
			delete(oldContainers, n)
		} else {
			specBytes, err := json.Marshal(*spec)
			if err != nil {
				return nil, wrapPodmanError(err, "Error when marshaling spec")
			}
			spec.Annotations = map[string]string{"sloop_config": string(specBytes)}
			id_, err := containers.CreateWithSpec(conn, spec, &containers.CreateOptions {
			})
			if err != nil {
				return nil, wrapPodmanError(err, "Error when creating creating container")
			}
			id = id_.ID
		}
		newContainers[n] = id
	}
	return newContainers, nil
}

func createServices(conn context.Context, services map[string]cue.Service, containers map[string]string) (map[string]string, error) {
	newServices := make(map[string]string)
	for n, s := range services {
		true_ := true
		empty := ""
		id := containers[n]
		report, err := generate.Systemd(conn, id, &generate.SystemdOptions {
			NoHeader: &true_,
			ContainerPrefix: &empty,
			PodPrefix: &empty,
			Separator: &empty,
			Wants: &s.Wants,
			Requires: &s.Requires,
			After: &s.After,
		})
		if err != nil {
			return nil, wrapPodmanError(err, "Error when generating service")
		}
		newServices[n] = "# sloop_service\n" + report.Units[id]
	}
	return newServices, nil
}

func findExistingServices(fullUnitsDir string) (map[string]string, error) {
	services := make(map[string]string)
	err := os.MkdirAll(fullUnitsDir, os.ModePerm)
	if err != nil {
		return nil, wrapPodmanError(err, "Error when creating systemd unit files directory")
	}
	files, err := ioutil.ReadDir(fullUnitsDir)
	if err != nil {
		return nil, wrapPodmanError(err, "Error when listing systemd unit directory")
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".service") {
			n := strings.TrimSuffix(f.Name(), ".service")
			contentB, err := os.ReadFile(fullUnitsDir + f.Name())
			content := string(contentB)
			if err != nil {
				return nil, wrapPodmanError(err, "Error when reading systemd unit file")
			}
			if strings.HasPrefix(content, "# sloop_service\n") {
				services[n] = content
			}
		}
	}
	return services, nil
}

func dedupNewOldServices(oldServices map[string]string, newServices map[string]string) {
	for s, oldc := range oldServices {
		if newServices[s] == oldc {
			delete(oldServices, s)
			delete(newServices, s)
		}
	}
}


func deleteOldServices(systemd *dbus.Conn, oldServices map[string]string) error {
	for olds := range oldServices {
		statuses, err := systemd.ListUnitsByNamesContext(context.Background(), []string{olds + ".service"})
		if err != nil {
			return wrapPodmanError(err, "Error when listing unit")
		}
		fmt.Println(statuses[0])
		if statuses[0].ActiveState == "active" {
			// STOP unit
			wait := make(chan string)
			systemd.StopUnitContext(context.Background(), olds + ".service", "replace", wait)
			fmt.Printf("stopping %s...\n", olds)
			res := <- wait
			if res != "done" {
				return errors.New("Error when stopping unit")
			}
			fmt.Printf("done\n")
		}
		if statuses[0].LoadState != "not-found" {
			// Disable unit
			_, err = systemd.DisableUnitFilesContext(context.Background(), []string{olds + ".service"}, false)
			if err != nil {
				return wrapPodmanError(err, "Error when disabling unit")
			}
		}
		// Remove unit file
		os.Remove(olds + ".service")
	}
	return nil
}
func deleteOldContainers(conn context.Context, oldContainers map[string]string) error {
	for _, c := range oldContainers {
		_, err := containers.Remove(conn, c, nil)
		if err != nil {
			return wrapPodmanError(err, "Error when removing container")
		}
	}
	return nil
}

func startServices(systemd *dbus.Conn, fullUnitsDir string, newServices map[string]string) error {
	for name, content := range newServices {
		fullPath := fullUnitsDir + name+".service"
		//create service file
		err := os.WriteFile(fullPath, []byte(content), 0o655)
		if err != nil {
			return wrapPodmanError(err, "Error when writing service file")
		}

		//enable service
		_, _, err = systemd.EnableUnitFilesContext(context.Background(), []string{fullPath}, false, true)
		if err != nil {
			return wrapPodmanError(err, "Error when enabling service file")
		}
		systemd.ReloadContext(context.Background())
		//start service
		wait := make(chan string)
		systemd.StartUnitContext(context.Background(), name + ".service", "replace", wait)
		fmt.Printf("starting %s...\n", name)
		res := <- wait
		if res != "done" {
			return errors.New("Error when starting unit")
		}
		fmt.Printf("done\n")
	}
	return nil
}

func Purge(config cue.Config) error {
	pwd, err := os.Getwd()
	if err != nil {
		return wrapPodmanError(err, "Error when getting pwd")
	}
	fullUnitsDir := pwd + "/" + unitsDir
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		return wrapPodmanError(err, "Error when connecting to podman socket")
	}
	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return wrapPodmanError(err, "Error when connecting to systemd")
	}

	oldContainers, err := findAllContainers(conn, config.Services)
	if err != nil {
		return err
	}
	oldServices, err := findExistingServices(fullUnitsDir)
	if err != nil {
		return err
	}
	err = deleteOldServices(systemd, oldServices)
	if err != nil {
		return err
	}
	err = deleteOldContainers(conn, oldContainers)
	if err != nil {
		return err
	}
	return nil
}

func Execute(config cue.Config) error {
	pwd, err := os.Getwd()
	if err != nil {
		return wrapPodmanError(err, "Error when getting pwd")
	}
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		return wrapPodmanError(err, "Error when connecting to podman socket")
	}
	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return wrapPodmanError(err, "Error when connecting to systemd")
	}

	err = createVolumes(conn, config.Volumes)
	if err != nil {
		return err
	}
	err = createNetworks(conn, config.Networks)
	if err != nil {
		return err
	}
	builds, err := createImages(conn, config.Images)
	if err != nil {
		return err
	}
	oldContainers, err := findAllContainers(conn, config.Services)
	if err != nil {
		return err
	}
	newContainers, err := createContainers(conn, config.Services, builds, oldContainers)
	if err != nil {
		return err
	}
	newServices, err := createServices(conn, config.Services, newContainers)
	if err != nil {
		return err
	}
	fullUnitsDir := pwd + "/" + unitsDir
	oldServices, err := findExistingServices(fullUnitsDir)
	if err != nil {
		return err
	}
	dedupNewOldServices(oldServices, newServices)
	err = deleteOldServices(systemd, oldServices)
	if err != nil {
		return err
	}
	err = deleteOldContainers(conn, oldContainers)
	if err != nil {
		return err
	}
	err = startServices(systemd, fullUnitsDir, newServices)
	if err != nil {
		return err
	}

	return nil
}
