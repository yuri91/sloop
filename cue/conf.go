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
	Name string `json:"name"`
	Ip string `json:"ip"`
	Bridge Bridge `json:"bridge"`
}
type Host struct {
	Name string
	Interfaces map[string]Interface `json:"if"`
}
type File struct {
	Content string
	Permissions uint16
}
type PortBinding struct {
	Host uint16
	Service uint16
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
	Ports []PortBinding
	Host string
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
	Hosts map[string]Host `json:"$hosts"`
	Services map[string]Service `json:"$services"`
	Timers map[string]Timer `json:"$timers"`
}

