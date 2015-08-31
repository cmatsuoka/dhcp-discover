package main

import (
	"./dhcp"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

type option struct {
	Len  int
	Name string
}

var options map[byte]option
var messageType map[byte]string

func init() {
	options = map[byte]option{
		dhcp.PadOption:          {0, "Pad Option"},
		dhcp.Router:             {-1, "Router"},
		dhcp.SubnetMask:         {4, "Subnet Mask"},
		dhcp.DomainNameServer:   {-1, "Domain Name Server"},
		dhcp.HostName:           {-1, "Host Name"},
		dhcp.DomainName:         {-1, "Domain Name"},
		dhcp.BroadcastAddress:   {4, "Broadcast Address"},
		dhcp.StaticRoute:        {-1, "Static Route"},
		dhcp.IPAddressLeaseTime: {4, "IP Address Lease Time"},
		dhcp.DHCPMessageType:    {1, "DHCP Message Type"},
		dhcp.ServerIdentifier:   {4, "Server Identifier"},
		dhcp.RenewalTimeValue:   {4, "Renewal Time Value"},
		dhcp.RebindingTimeValue: {4, "Rebinding Time Value"},
		dhcp.VendorSpecific:     {-1, "Vendor Specific"},
		dhcp.NetBIOSNameServer:  {-1, "NetBIOS Name Server"},
		dhcp.DomainSearch:       {-1, "Domain Search"},
		dhcp.WebProxyServer:     {-1, "Web Proxy Server"},
	}

	messageType = map[byte]string{
		dhcp.DHCPDiscover: "DHCPDISCOVER",
		dhcp.DHCPOffer:    "DHCPOFFER",
		dhcp.DHCPRequest:  "DHCPREQUEST",
		dhcp.DHCPDecline:  "DHCPDECLINE",
		dhcp.DHCPAck:      "DHCPACK",
		dhcp.DHCPNack:     "DHCPNACK",
		dhcp.DHCPRelease:  "DHCPRELEASE",
	}
}

func b32(data []byte) uint32 {
	buf := bytes.NewBuffer(data)
	var x uint32
	binary.Read(buf, binary.BigEndian, &x)
	return x
}

func ip4(data []byte) string {
	var ip dhcp.IPv4Address
	copy(ip[:], data[0:4])
	return ip.String()
}

func parseOptions(p *dhcp.Packet) {
	opts := p.Options
	fmt.Println("Options:")
loop:
	for i := 0; i < len(opts); i++ {
		o := opts[i]

		switch o {
		case dhcp.EndOption:
			fmt.Print("End Option")
			break loop
		case dhcp.PadOption:
			continue
		}

		length := int(opts[i+1])
		_, ok := options[o]
		if ok && options[o].Len >= 0 && options[o].Len != length {
			fmt.Printf("corrupted option (%d,%d)\n",
				options[o].Len, length)
			break loop
		}

		if name := options[o].Name; name != "" {
			fmt.Printf("%24s : ", options[o].Name)
		} else {
			fmt.Printf("%24d : ", o)
		}

		switch o {
		case dhcp.DHCPMessageType:
			fmt.Print(messageType[opts[i+2]])
			break
		case dhcp.Router, dhcp.DomainNameServer, dhcp.NetBIOSNameServer:
			// Multiple IP addresses
			for n := 0; n < length; n += 4 {
				fmt.Print(ip4(opts[i+2+n:i+6+n]), " ")
			}
		case dhcp.ServerIdentifier, dhcp.SubnetMask, dhcp.BroadcastAddress:
			// Single IP address
			fmt.Print(ip4(opts[i+2:]))
			break
		case dhcp.IPAddressLeaseTime, dhcp.RenewalTimeValue, dhcp.RebindingTimeValue:
			// 32-bit integer
			fmt.Print(b32(opts[i+2:]))
			break
		case dhcp.HostName, dhcp.DomainName, dhcp.WebProxyServer:
			// String
			fmt.Print(string(opts[i+2 : i+2+length]))
			break
		case dhcp.DomainSearch:
			// Compressed domain names (RFC 1035)
			fmt.Print("[TODO RFC 1035 section 4.1.4]")
			break
		case dhcp.VendorSpecific:
			// Size only
			fmt.Printf("(%d bytes)", length)
			break
		}
		fmt.Println()

		i += 1 + length
	}
}

func showPacket(p *dhcp.Packet) {
	fmt.Println("Client IP address :", p.Ciaddr.String())
	fmt.Println("Your IP address   :", p.Yiaddr.String())
	fmt.Println("Server IP address :", p.Siaddr.String())
	fmt.Println("Relay IP address  :", p.Giaddr.String())
	parseOptions(p)
	fmt.Println()
}

func checkError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}

func getMAC(s string) (string, error) {
	ifaces, err := net.Interfaces()
	checkError(err)
	for _, i := range ifaces {
		if i.Name == s {
			return i.HardwareAddr.String(), nil
		}
	}
	return "", fmt.Errorf("%s: no such interface", s)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options]\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	var iface string
	var secs int

	flag.StringVar(&iface, "i", "", "network `interface` to use")
	flag.IntVar(&secs, "t", 5, "timeout in seconds")
	flag.Parse()

	if iface == "" {
		usage()
		os.Exit(1)
	}

	mac := ""
	timeout := time.Duration(secs) * time.Second

	mac, err := getMAC(iface)
	checkError(err)

	fmt.Printf("Interface: %s [%s]\n", iface, mac)

	// Set up server
	addr, err := net.ResolveUDPAddr("udp4", ":68")
	checkError(err)
	conn, err := net.ListenUDP("udp4", addr)
	checkError(err)
	defer conn.Close()

	// Send discover packet
	p := dhcp.NewDiscoverPacket()
	p.ParseMAC(mac)

	fmt.Println("\n>>> Send DHCP discover")
	showPacket(&p.Packet)
	err = p.Send()
	checkError(err)

	t := time.Now()
	for time.Since(t) < timeout {
		var o dhcp.Packet
		remote, err := o.Receive(conn, timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err.Error())
			break
		}
		fmt.Println("\n<<< Receive DHCP offer from", remote.IP.String())
		showPacket(&o)
	}
	fmt.Println("No more offers.")
}
