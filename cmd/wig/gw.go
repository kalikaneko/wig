package main

import (
	"context"
	"flag"

	"git.autistici.org/ai3/attic/wig/collector"
	"git.autistici.org/ai3/attic/wig/datastore"
	"git.autistici.org/ai3/attic/wig/datastore/peerdb"
	"git.autistici.org/ai3/attic/wig/gateway"
	"git.autistici.org/ai3/attic/wig/util"
	"github.com/google/subcommands"
)

type gwCommand struct {
	util.ClientTLSFlags

	wireguardPort int
	logURL        urlFlag
	ifName        string
	ip            ipNetFlag
	ip6           ipNetFlag
	fwmark        int
	priv          privateKeyFlag
}

func (c *gwCommand) Name() string     { return "gw" }
func (c *gwCommand) Synopsis() string { return "run the gateway node" }
func (c *gwCommand) Usage() string {
	return `gw
        Run the VPN gateway node.

`
}

func (c *gwCommand) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.ifName, "iface", "wg0", "interface `name`")
	f.IntVar(&c.wireguardPort, "port", 4004, "`port` for the Wireguard protocol")
	f.Var(&c.ip, "ip", "IPv4 `addr`ess and range for the VPN interface (CIDR)")
	f.Var(&c.ip6, "ip6", "IPv6 `addr`ess and range for the VPN interface (CIDR)")
	f.IntVar(&c.fwmark, "fwmark", 0, "set fwmark flag to `N` for Wireguard traffic on this interface")
	f.Var(&c.logURL, "log-url", "`URL` for the log API")
	f.Var(&c.priv, "private-key", "`path` to the file containing the private key")

	c.ClientTLSFlags.SetFlags(f)
}

func (c *gwCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if c.ifName == "" {
		return syntaxErr("must specify --iface")
	}
	if c.ip.CIDR == nil {
		return syntaxErr("must specify --ip")
	}
	if c.logURL == "" {
		return syntaxErr("must specify --log-url")
	}
	if string(c.priv) == "" {
		return syntaxErr("must specify a private key")
	}

	return fatalErr(c.run(ctx))
}

func (c *gwCommand) run(ctx context.Context) error {
	tlsConf, err := c.TLSClientConfig()
	if err != nil {
		return err
	}

	rlog := peerdb.NewRemoteLog(string(c.logURL), tlsConf)
	rstats := collector.NewStatsCollectorStub(string(c.logURL), tlsConf)

	intf := &datastore.Interface{
		Name:       c.ifName,
		IP:         c.ip.CIDR,
		IP6:        c.ip6.CIDR,
		Fwmark:     c.fwmark,
		PrivateKey: string(c.priv),
	}

	gw, err := gateway.New(c.wireguardPort, intf, rstats)
	if err != nil {
		return err
	}
	defer gw.Close()

	if err := peerdb.Follow(ctx, rlog, gw); err != nil {
		return err
	}

	return nil
}

func init() {
	subcommands.Register(&gwCommand{}, "")
}
