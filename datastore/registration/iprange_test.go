package registration

import (
	"net"
	"testing"
)

func TestIPGenerator(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("172.23.12.0/24")
	g := newGeneratorFromIPNet(*ipnet)

	for i := 0; i < 256; i++ {
		ip := g.Next()

		if ip == nil {
			if i != 254 {
				t.Fatalf("sequence terminated at index %d, not 254 as expected", i)
			}
			break
		}
		if ip.String() == "172.23.12.0" {
			t.Fatal("generated network address")
		}
		if ip.String() == "172.23.12.255" {
			t.Fatal("generated broadcast address")
		}
	}
}
