package collector

import (
	"log"
	"net"
	"os"

	"git.autistici.org/ai3/attic/wig/datastore/model"
	"github.com/oschwald/maxminddb-golang"
)

var defaultGeoIPPaths = []string{
	"/var/lib/GeoIP/GeoLite2-Country.mmdb",
	"/var/lib/GeoIP/GeoLite2-ASN.mmdb",
}

type ipRefiner struct {
	dbs []*maxminddb.Reader
}

func newIPRefiner(paths []string) (*ipRefiner, error) {
	if len(paths) == 0 {
		paths = defaultGeoIPPaths
	}

	var ipr ipRefiner
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		geo, err := maxminddb.Open(path)
		if err != nil {
			return nil, err
		}
		ipr.dbs = append(ipr.dbs, geo)
	}

	return &ipr, nil
}

func (r *ipRefiner) addIPInfo(sess *model.Session, ipStr string) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return
	}

	for _, db := range r.dbs {
		var record struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
			ASN struct {
				Number       string `maxminddb:"autonomous_system_number"`
				Organization string `maxminddb:"autonomous_system_organization"`
			} `maxminddb:"asn"`
		}
		err := db.Lookup(ip, &record)
		if err != nil {
			log.Printf("geoip lookup error for %s: %v", ipStr, err)
			continue
		}

		if record.Country.ISOCode != "" {
			sess.SrcCountry = record.Country.ISOCode
		}
		if record.ASN.Number != "" {
			sess.SrcASNum = record.ASN.Number
		}
		if record.ASN.Organization != "" {
			sess.SrcASOrg = record.ASN.Organization
		}
	}
}
