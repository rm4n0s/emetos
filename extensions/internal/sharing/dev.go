package sharing

import (
	"encoding/json"
	"net"
	"os"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
)

// DevInterfacesEnv is the environment variable that replaces the interfaces of this computer by a list in debug builds and builds
// with the emetos_dev tag (SPEC §15.2): the proofs and the real-input tests set it, so that the addresses on their screenshots are
// the documentation addresses of RFC 5737 and never those of the machine they ran on. A release build never reads it. It holds a
// JSON array such as
//
//	[{"name":"eth0","up":true,"defaultRoute":true,"addrs":["192.0.2.10/24"]},{"name":"docker0","up":true,"addrs":["203.0.113.9/24"]}]
//
// where "loopback": true marks a loopback interface.
const DevInterfacesEnv = "EMETOS_DEV_INTERFACES"

// devInterface is one entry of the list of DevInterfacesEnv.
type devInterface struct {
	Name         string   `json:"name"`
	Up           bool     `json:"up"`
	Loopback     bool     `json:"loopback"`
	DefaultRoute bool     `json:"defaultRoute"`
	Addrs        []string `json:"addrs"`
}

// DefaultInterfaces is the Interfaces the real hubs use: the list of DevInterfacesEnv in a debug or emetos_dev build that sets it,
// else NetInterfaces.
func DefaultInterfaces() Interfaces {
	if text := os.Getenv(DevInterfacesEnv); text != "" && (foundation.DebugMode || devBuild) {
		return func() ([]Interface, error) { return parseDevInterfaces(text) }
	}
	return NetInterfaces
}

// parseDevInterfaces reads the list of DevInterfacesEnv. It fails with SharingDevInterfacesInvalid for text that is not that list
// or an address that is not an address with its prefix length ("192.0.2.10/24").
func parseDevInterfaces(text string) ([]Interface, error) {
	var in []devInterface
	if err := json.Unmarshal([]byte(text), &in); err != nil {
		return nil, errors.NewErr("SharingDevInterfacesInvalid", err).SetMetadata("variable", DevInterfacesEnv)
	}
	out := make([]Interface, 0, len(in))
	for _, d := range in {
		ifc := Interface{Name: d.Name, Up: d.Up, Loopback: d.Loopback, DefaultRoute: d.DefaultRoute}
		for _, a := range d.Addrs {
			ip, ipnet, err := net.ParseCIDR(a)
			if err != nil {
				return nil, errors.NewErr("SharingDevInterfacesInvalid", err).SetMetadata("variable", DevInterfacesEnv, "address", a)
			}
			ipnet.IP = ip
			ifc.Addrs = append(ifc.Addrs, ipnet)
		}
		out = append(out, ifc)
	}
	return out, nil
}
