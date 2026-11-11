package adapters

import (
	"errors"
	"fmt"
	"strings"
)

const apSharingSubnet = "192.168.226.0/24"
const apSharingGateway = "192.168.226.1"
const apSharingTable = "427226"
const apSharingPriority = "5000"
const apSharingProtocol = "242"
const apSharingForwardChain = "NCC_AP_FWD_V1"
const apSharingDNSChain = "NCC_AP_DNS_V1"
const apSharingNATChain = "NCC_AP_NAT_V1"

type apSharingPlan struct {
	filter   map[string][][]string
	nat      map[string][][]string
	routes   [][]string
	dns      string
	upstream *APSharingUpstream
}

func apSharingMarker(token string) []string {
	return []string{"-m", "comment", "--comment", "ncc-ap:" + token, "-j", "RETURN"}
}
func apBuildSharingPlan(token string, u *APSharingUpstream) (apSharingPlan, error) {
	if len(token) != 32 || strings.Trim(token, "0123456789abcdef") != "" {
		return apSharingPlan{}, errors.New("AP_SHARING_OWNER_INVALID")
	}
	p := apSharingPlan{filter: map[string][][]string{}, nat: map[string][][]string{}, routes: [][]string{{"blackhole", "default", "metric", "32767", "proto", apSharingProtocol}}}
	forward := [][]string{}
	nat := [][]string{}
	if u != nil {
		if !apSharingInterfaceName.MatchString(u.iface) || !u.address.Is4() || len(u.dns) == 0 {
			return apSharingPlan{}, errors.New("AP_UPSTREAM_PROOF_INVALID")
		}
		p.upstream = u
		forward = append(forward, []string{"-i", apInterface, "-s", apSharingSubnet, "-o", u.iface, "-j", "ACCEPT"}, []string{"-i", u.iface, "-d", apSharingSubnet, "-o", apInterface, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"})
		nat = append(nat, []string{"-s", apSharingSubnet, "-o", u.iface, "-j", "MASQUERADE"})
		p.routes = append(p.routes, []string{u.prefix.String(), "dev", u.iface, "scope", "link", "src", u.address.String(), "proto", apSharingProtocol})
		route := []string{"default"}
		if u.gateway.IsValid() {
			route = append(route, "via", u.gateway.String())
		}
		route = append(route, "dev", u.iface, "src", u.address.String(), "metric", "100", "proto", apSharingProtocol)
		p.routes = append(p.routes, route)
		for _, address := range u.dns {
			p.dns += "server=" + address.String() + "@" + u.iface + "\n"
		}
	}
	forward = append(forward, []string{"-j", "DROP"}, apSharingMarker(token))
	p.filter[apSharingForwardChain] = forward
	p.filter[apSharingDNSChain] = [][]string{{"-i", apInterface, "-s", apSharingSubnet, "-d", apSharingGateway, "-p", "udp", "--dport", "53", "-j", "ACCEPT"}, {"-i", apInterface, "-s", apSharingSubnet, "-d", apSharingGateway, "-p", "tcp", "--dport", "53", "-j", "ACCEPT"}, apSharingMarker(token)}
	p.nat[apSharingNATChain] = append(nat, apSharingMarker(token))
	return p, nil
}
func apSharingJumps(token string) [][]string {
	return [][]string{{"filter", "FORWARD", "-i", apInterface, "-m", "comment", "--comment", "ncc-ap:" + token, "-j", apSharingForwardChain}, {"filter", "FORWARD", "-o", apInterface, "-m", "comment", "--comment", "ncc-ap:" + token, "-j", apSharingForwardChain}, {"filter", "INPUT", "-i", apInterface, "-m", "comment", "--comment", "ncc-ap:" + token, "-j", apSharingDNSChain}, {"nat", "POSTROUTING", "-s", apSharingSubnet, "-m", "comment", "--comment", "ncc-ap:" + token, "-j", apSharingNATChain}}
}
func (p apSharingPlan) restoreText() string {
	var out strings.Builder
	for _, table := range []struct {
		name   string
		chains []string
		rules  map[string][][]string
	}{{"filter", []string{apSharingForwardChain, apSharingDNSChain}, p.filter}, {"nat", []string{apSharingNATChain}, p.nat}} {
		fmt.Fprintf(&out, "*%s\n", table.name)
		for _, chain := range table.chains {
			fmt.Fprintf(&out, ":%s - [0:0]\n-F %s\n", chain, chain)
			for _, args := range table.rules[chain] {
				fmt.Fprintf(&out, "-A %s %s\n", chain, strings.Join(args, " "))
			}
		}
		out.WriteString("COMMIT\n")
	}
	return out.String()
}
