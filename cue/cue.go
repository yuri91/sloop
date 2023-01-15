package cue

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"

	"github.com/samber/lo"
)

const typesStr = `
import $__net "net"
#Volume: {
	name: string
}
#IPPrefix: >0 & <32
#Bridge: {
	name: string
	ip: $__net.IP & string | *"0.0.0.0"
	prefix: #IPPrefix
	...
}
#Interface: {
	name: string
	type: "bridge"
	bridge: #Bridge
	ip: $__net.IP & string | *"0.0.0.0"
	...
}
#Network: {
	private: bool | *true
	ifs: [Name=_]: #Interface & {name: string | *Name}
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
	net?: #Network
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

$service: [Name=_]: S=#Service & {
	name: string | *strings.Replace(Name,"_","-",-1)
	_volumeCheck: {
		for k, v in S.image.volumes {
			"\(v.name).is_in_$volume": [ for k1, v1 in $volume if v1.name == v.name {v1}] & [v]
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
$services: {
	for _, s in $service {
		"\(s.name)": {
			name: s.name
			type: s.type
			exec: s.exec
			if s.net != _|_ {
				net: s.net
			}
			if s.net == _|_ {
				net: {
					private: false
				}
			}
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

type BridgePeer struct {
	Host string
	Iface *Interface
}
type BridgeData struct {
	Bridge
	Peers []BridgePeer `json:"-"`
}
type HostData struct {
	Name string `json:"name"`
	Net Network `json:"net"`
}
func nonull(ptr []byte, msg string) {
	if ptr == nil {
		panic(msg)
	}
}
func ip2int(ipStr string) uint32 {
	ip := net.ParseIP(ipStr)[12:]
	nonull(ip, "ipStr must be a valid ip")
	return binary.BigEndian.Uint32(ip)
}
func ip2intPrefix(ipStr string, prefix uint32) uint32 {
	ip := net.ParseIP(ipStr)
	nonull(ip, "ipStr must be a valid ip")
	mask := net.CIDRMask(int(prefix), 32)
	nonull(mask, "prefix must be a valid subnet prefix")
	masked := ip.Mask(mask)
	nonull(masked, "mask operation cannot fail")
	return binary.BigEndian.Uint32(masked)
}

func int2ip(nn uint32) string {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, nn)
	return ip.String()
}
func hashBits(input string, mask uint32) uint32 {
	sha := sha256.Sum256([]byte(input))
	n := binary.BigEndian.Uint32(sha[0:4])
	n = n & mask
	return n
}
func allocateIp(allocMap map[uint32]bool, start uint32, end uint32,name string) (uint32, error) {
	mask := end-start-1
	candidate := start + 1 + hashBits(name, mask)
	if candidate >= end {
		candidate = start + 1
	}
	for {
		_, exists := allocMap[candidate]
		if !exists {
			break;
		}
		candidate++
		if candidate >= end {
			return 0, IpInjectError.New("Ip range exausted")
		}
	}
	allocMap[candidate] = true
	return candidate, nil
}
func injectIPs(value cue.Value) (*cue.Value, error) {
	bridgeMap := make(map[string]BridgeData)
	bridgesVal := value.LookupPath(cue.ParsePath("$bridge"))
	err := bridgesVal.Decode(&bridgeMap)
	if err != nil {
		return nil, DecodeError.Wrap(err, "Error during decoding into go type")
	}
	bridgeMap = lo.MapKeys(bridgeMap, func(v BridgeData, _ string) string {
		return v.Name
	})
	hostMap := make(map[string]HostData)
	hostsVal := value.LookupPath(cue.ParsePath("$service"))
	err = hostsVal.Decode(&hostMap)
	if err != nil {
		return nil, DecodeError.Wrap(err, "Error during decoding into go type")
	}
	for _, host := range hostMap {
		for _, iface := range host.Net.Interfaces {
			b := bridgeMap[iface.Bridge.Name]
			b.Peers = append(b.Peers, BridgePeer{host.Name, iface})
			bridgeMap[iface.Bridge.Name] = b
		}
	}
	for _, bridge := range bridgeMap {
		start := ip2intPrefix(bridge.Ip, uint32(bridge.Prefix))
		end := start + (1<<bridge.Prefix)
		ip := ip2int(bridge.Ip)
		allocMap := make(map[uint32]bool)
		allocMap[ip] = true
		if ip := ip2int(bridge.Ip); ip != 0 {
			allocMap[ip] = true
		}
		for _, i := range bridge.Peers {
			if ip := ip2int(i.Iface.Ip); ip != 0 {
				allocMap[ip] = true
			}
		}
		for _, i := range bridge.Peers {
			if ip := ip2int(i.Iface.Ip); ip != 0 {
				continue;
			}
			ip, err := allocateIp(allocMap, start, end, i.Host+i.Iface.Name)
			if err != nil {
				return nil, err
			}
			i.Iface.Ip = int2ip(ip)
		}
	}
	newVal := value.FillPath(cue.ParsePath("$service"), hostMap)
	return &newVal, nil
}
func GetCueConfig(path string) (*cue.Value, error) {
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

	value = value.Unify(types)
	valueAddr, err := injectIPs(value)
	if err != nil {
		return nil, err
	}
	return valueAddr, nil
}

func GetGoConfig(scope cue.Value) (*Config, error) {
	ctx := scope.Context()
	value := ctx.CompileString(goTypesStr, cue.Filename("sloop_go_types.cue"), cue.Scope(scope))
	if value.Err() != nil {
		return nil, ConvertError.Wrap(value.Err(), "Error go type conversion")
	}

	// Validate the value
	err := value.Validate(cue.Concrete(true), cue.ResolveReferences(true))
	if err != nil {
		return nil, ValidateError.Wrap(err, "Error in validation")
	}
	conf := Config{}
	err = value.Decode(&conf)
	if err != nil {
		return nil, DecodeError.Wrap(err, "Error during decoding into go type")
	}
	return &conf,nil
}

func GetConfig(path string) (*Config, error) {
	scope, err := GetCueConfig(path)
	if err != nil {
		return nil, err
	}
	return GetGoConfig(*scope)
}

func Print(value cue.Value, pathStr string) {
	path := cue.ParsePath(pathStr)
	print := value.LookupPath(path);

	syn := print.Syntax(
		cue.Final(),
		cue.Concrete(false),
		cue.Definitions(false),
		cue.Hidden(true),
		cue.Optional(true),
	)
	bs, _ := format.Node(syn)
	fmt.Println(string(bs));
}
