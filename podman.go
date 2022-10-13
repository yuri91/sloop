package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"reflect"
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
func buildImage(conn context.Context, i Image, containerTemplate *template.Template) (*entities.BuildReport, error) {
	var buf bytes.Buffer
	err := containerTemplate.Execute(&buf, i)
	if err != nil {
		return nil, wrapPodmanError(err, "Error when executing Containerfile template")
	}
	containerStr := buf.String()
	fmt.Println(containerStr)
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
		},
	})
	if err != nil {
		return report, wrapPodmanError(err, "Cannot build image")
	}
	// TODO build image
	return report, nil
}
func getVolumes(s Service) []*specgen.NamedVolume {
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
func getMounts(s Service) []spec.Mount {
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
func getNetworks(s Service) map[string]nettypes.PerNetworkOptions {
	ret := make(map[string]nettypes.PerNetworkOptions, len(s.Networks))
	for _, n := range s.Networks {
		ret[n] = nettypes.PerNetworkOptions{}
	}
	return ret
}
func getPortMappings(s Service) []nettypes.PortMapping {
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
	var origSpec specgen.SpecGenerator
	err = json.Unmarshal([]byte(origSpecStr), &origSpec)
	if err != nil {
		return false, wrapPodmanError(err, "Error while unmarshaling spec")
	}
	delete(origSpec.Annotations,"sloop_config")
	return reflect.DeepEqual(origSpec, spec), nil
}

func run(config Config) error {
	pwd, err := os.Getwd()
	if err != nil {
		return wrapPodmanError(err, "Error when getting pwd")
	}
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		return wrapPodmanError(err, "Error when connecting to podman socket")
	}

	for _, v := range config.Volumes {
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
	for _, v := range config.Networks {
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
	containerTemplate := template.Must(template.New("containerfile").Funcs(template.FuncMap{"StringsJoin": stringsJoin, "ConvPath":convPath, "Octal": octal}).Parse(containerTemplateStr))
	if err != nil {
		return wrapPodmanError(err, "Error when creating Containerfile template")
	}

	builds := make(map[string]string)
	for n, i := range config.Images {
		id, err := buildImage(conn, i, containerTemplate)
		if err != nil {
			return err
		}
		builds[n] = id.ID
	}

	oldContainers := make(map[string]string)
	for n, _ := range config.Services {
		true_ := true
		list, err := containers.List(conn, &containers.ListOptions {
			All: &true_,
			Filters: map[string][]string{
				"label": {fmt.Sprintf("sloop_service=%s", n)},
			},
		})
		if err != nil {
			return wrapPodmanError(err, "Error when listing existing containers")
		}
		if len(list) > 1 {
			return fmt.Errorf("Multiple existing containers for one name")
		}
		if len(list) == 1 {
			oldContainers[n] = list[0].ID
		}
	}
	services := make(map[string]string)
	for n, s := range config.Services {
		spec := specgen.NewSpecGenerator(builds[s.Image], false)
		spec.Volumes = getVolumes(s)
		spec.Mounts = getMounts(s)
		spec.Networks = getNetworks(s)
		spec.PortMappings = getPortMappings(s)
		spec.Labels = map[string]string{"sloop_service": n}
		exists := false
		if oldc, ok := oldContainers[n]; ok {
			exists, err = matchSpec(conn, oldc, *spec)
		}
		if err != nil {
			return err
		}
		var id string
		if exists {
			id = oldContainers[n]
			delete(oldContainers, n)
		} else {
			specBytes, err := json.Marshal(*spec)
			if err != nil {
				return wrapPodmanError(err, "Error when marshaling spec")
			}
			spec.Annotations = map[string]string{"sloop_config": string(specBytes)}
			id_, err := containers.CreateWithSpec(conn, spec, &containers.CreateOptions {
			})
			if err != nil {
				return wrapPodmanError(err, "Error when creating creating container")
			}
			id = id_.ID
		}
		true_ := true
		empty := ""
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
			return wrapPodmanError(err, "Error when generating service")
		}
		services[n] = "# sloop_service\n" + report.Units[id]
	}
	oldservices := make(map[string]string)
	fullUnitsDir := pwd + "/" + unitsDir
	err = os.MkdirAll(fullUnitsDir, os.ModePerm)
	if err != nil {
		return wrapPodmanError(err, "Error when creating systemd unit files directory")
	}
	files, err := ioutil.ReadDir(fullUnitsDir)
	if err != nil {
		return wrapPodmanError(err, "Error when listing systemd unit directory")
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".service") {
			n := strings.TrimSuffix(f.Name(), ".service")
			contentB, err := os.ReadFile(fullUnitsDir + f.Name())
			content := string(contentB)
			if err != nil {
				return wrapPodmanError(err, "Error when reading systemd unit file")
			}
			if strings.HasPrefix(content, "# sloop_service\n") {
				oldservices[n] = content
			}
		}
	}
	systemd, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return wrapPodmanError(err, "Error when connecting to systemd")
	}
	for olds, oldc := range oldservices {
		if services[olds] == oldc {
			continue
		}
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
	for _, c := range oldContainers {
		_, err = containers.Remove(conn, c, nil)
		if err != nil {
			return wrapPodmanError(err, "Error when removing container")
		}
	}
	for name, content := range services {
		if oldservices[name] == content {
			continue
		}
		fullPath := fullUnitsDir + name+".service"
		//create service file
		err = os.WriteFile(fullPath, []byte(content), 0o655)
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
