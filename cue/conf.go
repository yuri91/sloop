package cue

type Volume struct {
	Name string
}
type Bridge struct {
	Name string `json:"name"`
	Ip string `json:"ip"`
	Prefix int `json:"prefix"`
}
type Interface struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Ip string `json:"ip"`
	Bridge Bridge `json:"bridge"`
}
type Network struct {
	Interfaces map[string]*Interface `json:"ifs"`
	Private bool `json:"private"`
}
type File struct {
	Content string
	Permissions uint16
}
type VolumeMapping struct {
	Name string
	Dest string
}
type Image struct {
	From string
	Files map[string]File
	Env map[string]string
	Volumes []VolumeMapping
}
type Exec struct {
	Start []string
	Reload []string
}
type Service struct {
	Name  string
	Image Image
	Exec Exec
	Capabilities []string
	Net Network
	Type string
	Enable bool
	Wants []string
	Requires []string
	After []string
}
type Cmd struct {
	Service string
	Action string
}
type Timer struct {
	Name string
	Run []Cmd
	OnCalendar []string
	OnActiveSec []string
	Persistent bool
}
type Config struct {
	Volumes map[string]Volume `json:"$volumes"`
	Bridges map[string]Bridge `json:"$bridges"`
	Services map[string]Service `json:"$services"`
	Timers map[string]Timer `json:"$timers"`
}

