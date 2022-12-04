package cue

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
//	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
)

type cueError struct {
	err error
	msg string
}
func (e cueError) Error() string {
	return e.msg + ": " + e.err.Error()
}
func wrapCueError(err error, msg string) (Config, cueError) {
	return Config{}, cueError{err, msg}
}

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
	name: string
	from: string
	files: [string]:  #File
	env: [string]:    string
	#mounts: [string]: string
	...
}
#Service: {
	name:  string
	cmd: [...string]
	image: #Image
	volumes: [string]: #Volume
	ports: [...#PortBinding]
	host: #Host
	wants: [...#Dependency]
	requires: [...#Dependency]
	after: [...#Dependency]
	...
}
#UnitName:   =~"^(\\.service)|(\\.target)|(\\.socket)$"
#Dependency: #Service | #UnitName

`
const constraintsStr = `
$volume: [Name=_]: #Volume & {name: string | *Name}

$bridge: [Name=_]: #Bridge & {name: string | *Name}

$host: [Name=_]: #Host & {name: string | *Name}

$image: [Name=_]: #Image & {name: string | *Name}

$service: [Name=_]: S=#Service & {
	name: string | *Name
	_volumeCheck: {
		for k, v in S.volumes {
			"\(v.name).is_in_$volume": [ for k1, v1 in $volume if v1.name == v.name {v1}] & [v]
		}
	}
	_hostCheck: {
		"\(S.host.name).is_in_host": [ for k, v in $host if v.name == S.host.name {v}] & [S.host]
	}
	_mountCheck: [
		for k, v in S.image.#mounts {
			"\(k):\(v).is_mounted": [ for k1, v1 in S.volumes if k1 == v {v}] & [v]
		},
	]

}
`
const goTypesStr = `
#GoImage: #Image & {
	files: [string]:  #File
	$files: [string]:  #File
	$files: {
		for p,f in files {
			"\(p)": {
				content: string & (f.content | f)
				permissions: uint16 & (f.permissions | *0o666)
			}
		}
	}
}
#GoService: #Service & {
	image: #Image
	volumes: [string]: #Volume
	host: #Host
	ports: [...#PortBinding]
	wants: [...#Dependency]
	requires: [...#Dependency]
	after: [...#Dependency]

	$image: image.name
	$ports: [
		for p in ports {
			{
				host:  p.host | p
				service: p.service | p
			}
		}
	]
	$volumes: [
		for p,v in volumes {
			{
				name: v.name
				dest: p
			}
		}
	]
	$host: host.name
	$wants: [ for w in wants {(w & string) | (w.name + ".service")}]
	$requires: [ for r in requires {(r & string) | (r.name + ".service")}]
	$after: [ for a in after {(a & string) | (a.name + ".service")}]
}
#GoNetdev: #Netdev & {
	bridge: #Bridge
	$bridge: bridge.name
}
#GoHost: #Host & {
	netdevs: [string]: #Netdev
	$netdevs: [ for k, v in netdevs {v & #GoNetdev}]
}

$volumes: $volume
$bridges: $bridge
$hosts: {
	for k, v in $host {
		"\(k)": v&#GoHost
	}
}
$images: {
	for k, v in $image {
		"\(k)": v&#GoImage
	}
}
$services: {
	for k, v in $service {
		"\(k)": v&#GoService
	}
}
`

func GetConfig(path string) (Config, error) {
	// We need a cue.Context, the New'd return is ready to use
	ctx := cuecontext.New()

	types := ctx.CompileString(typesStr, cue.Filename("sloop_types.cue"))
	constraints := ctx.CompileString(constraintsStr, cue.Filename("sloop_constraints.cue"), cue.Scope(types))

	// Load Cue files into Cue build.Instances slice
	// the second arg is a configuration object, we'll see this later
	bis := load.Instances([]string{path}, &load.Config {
	})
	bi := bis[0]

	// check for errors on the instance
	// these are typically parsing errors
	if bi.Err != nil {
		return wrapCueError(bi.Err, "Error during load")
	}

	// Use cue.Context to turn build.Instance to cue.Instance
	value := ctx.BuildInstance(bi, cue.Scope(types))
	if value.Err() != nil {
		return wrapCueError(value.Err(), "Error during build")
	}

	value = value.Unify(constraints)
	if value.Err() != nil {
		return wrapCueError(value.Err(), "Error during constraints check")
	}

	scope := value.Unify(types)
	value = ctx.CompileString(goTypesStr, cue.Filename("sloop_go_types.cue"), cue.Scope(scope))
	if value.Err() != nil {
		return wrapCueError(value.Err(), "Error during go type conversion")
	}

	// Validate the value
	err := value.Validate(cue.Concrete(true), cue.ResolveReferences(true))
	if err != nil {
		return wrapCueError(err, "Error during validate")
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
		return wrapCueError(err, "Error during decoding to go type")
	}
	return conf,nil
}
