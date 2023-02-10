package registration

import (
	"math/big"
	"net"
)

type ipNetGenerator struct {
	net.IPNet
	count *big.Int

	//state
	idx     *big.Int
	current net.IP
}

func newGeneratorFromIPNet(ipNet net.IPNet) *ipNetGenerator {
	ones, bits := ipNet.Mask.Size()

	newIP := make(net.IP, len(ipNet.IP))
	copy(newIP, ipNet.IP)
	incrementIP(newIP)

	count := big.NewInt(0)
	count.Exp(big.NewInt(2), big.NewInt(int64(bits-ones)), nil)
	count.Sub(count, big.NewInt(1))

	return &ipNetGenerator{
		IPNet:   ipNet,
		count:   count,
		idx:     big.NewInt(1),
		current: newIP,
	}
}

//Next returns the next net.IP in the subnet
func (g *ipNetGenerator) Next() net.IP {
	g.idx.Add(g.idx, big.NewInt(1))
	if g.idx.Cmp(g.count) == 1 {
		return nil
	}
	current := make(net.IP, len(g.current))
	copy(current, g.current)
	incrementIP(g.current)

	return current
}

func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		//only add to the next byte if we overflowed
		if ip[i] != 0 {
			break
		}
	}
}
