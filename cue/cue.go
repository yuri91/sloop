package cue

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
//	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
)

const typesStr = `
#Volume: {
	name: string
}
#Bridge: {
	name: string
	ip: string
}
#Netdev: {
	name: string
	type: "veth"
	bridge: #Bridge
	ip: string
	...
}
#Host: {
	name: string
	netdevs: [Name=_]: #Netdev & {name: string | *Name}
	...
}

#File: {
	content:     string
	permissions: uint16
} | string

#PortBinding: {
	host:    uint16
	service: uint16
} | uint16

#Image: {
	from: string
	files: [string]:  #File
	env: [string]:    string
	volumes: [string]: #Volume
}
#Exec: {
	start: [...string] | *[]
	reload: [...string] | *[]
}
#Service: {
	name:  =~ "^[A-Za-z0-9-]+$"
	exec: #Exec
	image: #Image
	ports: [...#PortBinding]
	host: #Host | *null
	capabilities: [...string] | *[]
	type: "notify" | "oneshot" | *"simple"
	enable: bool | *true
	wants: [...#Dependency]
	requires: [...#Dependency]
	after: [...#Dependency]
	...
}

#Cmd: {
	service: #Service
	action: "start" | "reload"
}

#Timer: {
	name: string
	run: [...#Cmd]
	onCalendar: [...string] | *[]
	onActiveSec: [...string] | *[]
	persistent: bool | *true
	...
}

#UnitName:   =~"^(\\.service)|(\\.target)|(\\.socket)$"
#Dependency: #Service | #UnitName

`
const constraintsStr = `
import "strings"

$volume: [Name=_]: #Volume & {name: string | *strings.Replace(Name,"_","-",-1)}

$bridge: [Name=_]: #Bridge & {name: string | *strings.Replace(Name,"_","-",-1)}

$host: [Name=_]: #Host & {name: string | *strings.Replace(Name,"_","-",-1)}

$service: [Name=_]: S=#Service & {
	name: string | *strings.Replace(Name,"_","-",-1)
	_volumeCheck: {
		for k, v in S.image.volumes {
			"\(v.name).is_in_$volume": [ for k1, v1 in $volume if v1.name == v.name {v1}] & [v]
		}
	}
	_hostCheck: {
		if S.host != null {
			"\(S.host.name).is_in_host": [ for k, v in $host if v.name == S.host.name {v}] & [S.host]
		}
	}
}

$timer: [Name=_]: T=#Timer & {
	name: string | *strings.Replace(Name,"_","-",-1)
	_serviceCheck: {
		for r in T.run {
			"\(r.service.name).is_in_$service": [ for k, v in $service if v.name == r.service.name {v}] & [r.service]
		}
	}
}
`
const goTypesStr = `
#GoNetdev: #Netdev & {
	bridge: #Bridge
	$bridge: bridge.name
	bridgeIp: bridge.ip
}
#GoHost: #Host & {
	netdevs: [string]: #Netdev
	$netdevs: [ for k, v in netdevs {v & #GoNetdev}]
}

$volumes: {
	for _, v in $volume {
		"\(v.name)": v&#Volume
	}
}
$bridges: {
	for _, v in $bridge {
		"\(v.name)": v&#Bridge
	}
}
$hosts: {
	for _, v in $host {
		"\(v.name)": v&#GoHost
	}
}
$services: {
	for _, s in $service {
		"\(s.name)": {
			name: s.name
			type: s.type
			exec: s.exec
			image: {
				from: s.image.from
				env: s.image.env
				files: {
					for p,f in s.image.files {
						"\(p)": {
							if f.content != _|_ {
								content: f.content
								permissions: f.permissions
							}
							if f.content == _|_ {
								content: f
								permissions: 0o666
							}
						}
					}
				}
				volumes: [
					for p,v in s.image.volumes {
						{
							name: v.name
							dest: p
						}
					}
				]
			}
			ports: [
				for p in s.ports {
					{
						host:  p.host | p
						service: p.service | p
					}
				}
			]
			if s.host != null {
				host: s.host.name
			}
			if s.host == null {
				host: ""
			}
			enable: s.enable
			capabilities: s.capabilities
			wants: [ for w in s.wants {(w & string) | ("sloop-service-" + w.name + ".service")}]
			requires: [ for r in s.requires {(r & string) | ("sloop-service-" + r.name + ".service")}]
			after: [ for a in s.after {(a & string) | ("sloop-service-" + a.name + ".service")}]
		}
	}
}

$timers: {
	for _, t in $timer {
		"\(t.name)": {
			name: t.name
			run: [
				for r in t.run {
					{
						service: "sloop-service-" + r.service.name + ".service"
						action: r.action
					}
				}
			]
			onCalendar: t.onCalendar
			onActiveSec: t.onActiveSec
			persistent: t.persistent
		}
	}
}
`

func GetConfig(path string) (*Config, error) {
	// We need a cue.Context, the New'd return is ready to use
	ctx := cuecontext.New()

	types := ctx.CompileString(typesStr, cue.Filename("sloop_types.cue"))
	constraints := ctx.CompileString(constraintsStr, cue.Filename("sloop_constraints.cue"), cue.Scope(types))

	// Load Cue files into Cue build.Instances slice
	// the second arg is a configuration object, we'll see this later
	bis := load.Instances([]string{path}, &load.Config {
		Package: "main",
	})
	bi := bis[0]

	// check for errors on the instance
	// these are typically parsing errors
	if bi.Err != nil {
		return nil, LoadError.Wrap(bi.Err, "Error during load")
	}

	// Use cue.Context to turn build.Instance to cue.Instance
	value := ctx.BuildInstance(bi, cue.Scope(types))
	if value.Err() != nil {
		return nil, BuildError.Wrap(value.Err(), "Error during build")
	}

	value = value.Unify(constraints)
	if value.Err() != nil {
		return nil, ConstraintError.Wrap(value.Err(), "Error during constrain")
	}

	scope := value.Unify(types)
	value = ctx.CompileString(goTypesStr, cue.Filename("sloop_go_types.cue"), cue.Scope(scope))
	if value.Err() != nil {
		return nil, ConvertError.Wrap(value.Err(), "Error go type conversion")
	}

	// Validate the value
	err := value.Validate(cue.Concrete(true), cue.ResolveReferences(true))
	if err != nil {
		return nil, ValidateError.Wrap(err, "Error in validation")
	}

	//syn := value.Syntax(
	//	cue.Final(),
	//	cue.Concrete(true),
	//	cue.Definitions(false),
	//	cue.Hidden(true),
	//	cue.Optional(true),
	//)
	//bs, _ := format.Node(syn)
	//fmt.Println(string(bs));
	
	conf := Config{}
	err = value.Decode(&conf)
	if err != nil {
		return nil, DecodeError.Wrap(err, "Error during decoding into go type")
	}
	return &conf,nil
}
