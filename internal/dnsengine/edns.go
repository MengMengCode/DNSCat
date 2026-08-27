package dnsengine

import (
	"net"

	"github.com/miekg/dns"
)

// ExtractECS extracts EDNS Client Subnet (ECS) from a DNS message if present
func ExtractECS(req *dns.Msg) (net.IP, uint8) {
	if opt := req.IsEdns0(); opt != nil {
		for _, o := range opt.Option {
			if subnet, ok := o.(*dns.EDNS0_SUBNET); ok {
				return subnet.Address, subnet.SourceNetmask
			}
		}
	}
	return nil, 0
}

// AttachECSResponse attaches the ECS option in response with proper scope netmask
func AttachECSResponse(resp *dns.Msg, req *dns.Msg, scopeNetmask uint8) {
	if opt := req.IsEdns0(); opt != nil {
		for _, o := range opt.Option {
			if subnet, ok := o.(*dns.EDNS0_SUBNET); ok {
				respOpt := resp.IsEdns0()
				if respOpt == nil {
					resp.SetEdns0(4096, true)
					respOpt = resp.IsEdns0()
				}
				respSubnet := &dns.EDNS0_SUBNET{
					Code:          dns.EDNS0SUBNET,
					Family:        subnet.Family,
					SourceNetmask: subnet.SourceNetmask,
					SourceScope:   scopeNetmask,
					Address:       subnet.Address,
				}
				respOpt.Option = append(respOpt.Option, respSubnet)
				break
			}
		}
	}
}
