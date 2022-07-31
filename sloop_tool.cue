package sloop

import (
	"strings"
	"list"
	"text/tabwriter"
	"tool/cli"
	"tool/exec"
	// "tool/file"
)

volumes: [ for k, v in $volume {v}]
images: [ for k, v in $image {v}]
services: [ for k, v in $service {v}]

create_volume: exec.Run & {
	name:   string
	exists: bool
	cmd:    [
		if exists == false {
			{"podman volume create --label sloop \(name)"}
		},
		{"echo volume \(name) exists"},
	][0]
}
command: list: {
	task: print: cli.Print & {
		text: tabwriter.Write([
			"volumes:",
			for v in volumes {
				"\t\(v.name)"
			},
			"images:",
			for i in images {
				"\t\(i.name)"
			},
			"services:",
			for s in services {
				"\t\(s.name)"
			},
		])
	}
}

command: create_volumes: {
	task: list_volumes: exec.Run & {
		cmd:            "podman volume ls --format \"{{.Name}}\" --filter label=sloop"
		stdout:         string
		volumes_quoted: strings.Split(stdout, "\n")
		volumes: [ for v in volumes_quoted {strings.Trim(v, "\"")}]
	}
	task: {
		for v in volumes {
			"create_volume_\(v.name)": create_volume & {
				name:   v.name
				exists: list.Contains(task.list_volumes.volumes, v.name)
			}
		}
	}
}
