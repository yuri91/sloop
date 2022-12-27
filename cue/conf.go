package cue

type Volume struct {
	Name string
}
type Bridge struct {
	Name string
	Ip string
}
type Netdev struct {
	Name string
	Ip string
	Bridge string `json:"$bridge"`
	BridgeIp string `json:"bridgeIp"`
}
type Host struct {
	Name string
	Netdevs []Netdev `json:"$netdevs"`
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
type Service struct {
	Name  string
	Cmd []string
	From string
	Files map[string]File `json:"$files"`
	Env map[string]string
	Capabilities []string
	Volumes []VolumeMapping `json:"$volumes"`
	Ports []PortBinding `json:"$ports"`
	Host string `json:"$host"`
	Type string
	Enable bool
	Wants []string `json:"$wants"`
	Requires []string `json:"$requires"`
	After []string `json:"$after"`
}
type Config struct {
	Volumes map[string]Volume `json:"$volumes"`
	Bridges map[string]Bridge `json:"$bridges"`
	Hosts map[string]Host `json:"$hosts"`
	Services map[string]Service `json:"$services"`
}

