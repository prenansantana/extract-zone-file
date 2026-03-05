package resolver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var defaultRecordTypes = []uint16{
	dns.TypeSOA,
	dns.TypeNS,
	dns.TypeA,
	dns.TypeAAAA,
	dns.TypeCNAME,
	dns.TypeMX,
	dns.TypeTXT,
	dns.TypeSRV,
	dns.TypeCAA,
}

// RecordSet holds all queried DNS records for a domain.
type RecordSet struct {
	Domain      string
	Server      string
	Records     []dns.RR
	AXFRSuccess bool
}

// Resolve queries all supported record types for a domain.
func Resolve(domain, server string, tryAXFR bool, types string) (*RecordSet, error) {
	domain = dns.Fqdn(domain)

	recordTypes := defaultRecordTypes
	if types != "" {
		recordTypes = parseTypes(types)
		if len(recordTypes) == 0 {
			return nil, fmt.Errorf("no valid record types specified")
		}
	}

	// Find authoritative nameserver if none specified.
	if server == "" {
		ns, err := findAuthoritativeNS(domain)
		if err != nil {
			server = "8.8.8.8:53"
		} else {
			server = ns
		}
	}
	if !strings.Contains(server, ":") {
		server = server + ":53"
	}

	rs := &RecordSet{
		Domain: domain,
		Server: server,
	}

	// Try AXFR first.
	if tryAXFR {
		records, err := attemptAXFR(domain, server)
		if err == nil && len(records) > 0 {
			rs.Records = records
			rs.AXFRSuccess = true
			return rs, nil
		}
	}

	// Query each record type concurrently.
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, rrtype := range recordTypes {
		wg.Add(1)
		go func(t uint16) {
			defer wg.Done()
			records, err := queryRecords(domain, t, server)
			if err != nil || len(records) == 0 {
				return
			}
			mu.Lock()
			rs.Records = append(rs.Records, records...)
			mu.Unlock()
		}(rrtype)
	}

	wg.Wait()

	if len(rs.Records) == 0 {
		return nil, fmt.Errorf("no DNS records found for %s", domain)
	}

	return rs, nil
}

func queryRecords(domain string, rrtype uint16, server string) ([]dns.RR, error) {
	m := new(dns.Msg)
	m.SetQuestion(domain, rrtype)
	m.RecursionDesired = true

	c := new(dns.Client)
	c.Timeout = 5 * time.Second

	r, _, err := c.Exchange(m, server)
	if err != nil {
		return nil, err
	}
	if r.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("DNS query failed with rcode %d", r.Rcode)
	}

	return r.Answer, nil
}

func findAuthoritativeNS(domain string) (string, error) {
	m := new(dns.Msg)
	m.SetQuestion(domain, dns.TypeNS)
	m.RecursionDesired = true

	c := new(dns.Client)
	c.Timeout = 5 * time.Second

	r, _, err := c.Exchange(m, "8.8.8.8:53")
	if err != nil {
		return "", err
	}

	for _, ans := range r.Answer {
		if ns, ok := ans.(*dns.NS); ok {
			// Resolve the NS hostname to an IP.
			ip, err := resolveHost(ns.Ns)
			if err == nil {
				return ip + ":53", nil
			}
		}
	}

	return "", fmt.Errorf("no authoritative nameserver found for %s", domain)
}

func resolveHost(host string) (string, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(host), dns.TypeA)
	m.RecursionDesired = true

	c := new(dns.Client)
	c.Timeout = 5 * time.Second

	r, _, err := c.Exchange(m, "8.8.8.8:53")
	if err != nil {
		return "", err
	}

	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			return a.A.String(), nil
		}
	}

	return "", fmt.Errorf("could not resolve %s", host)
}

func attemptAXFR(domain, server string) ([]dns.RR, error) {
	t := new(dns.Transfer)
	m := new(dns.Msg)
	m.SetAxfr(domain)

	ch, err := t.In(m, server)
	if err != nil {
		return nil, err
	}

	var records []dns.RR
	for env := range ch {
		if env.Error != nil {
			return nil, env.Error
		}
		records = append(records, env.RR...)
	}

	return records, nil
}

func parseTypes(types string) []uint16 {
	typeMap := map[string]uint16{
		"A":     dns.TypeA,
		"AAAA":  dns.TypeAAAA,
		"CNAME": dns.TypeCNAME,
		"MX":    dns.TypeMX,
		"NS":    dns.TypeNS,
		"SOA":   dns.TypeSOA,
		"TXT":   dns.TypeTXT,
		"SRV":   dns.TypeSRV,
		"CAA":   dns.TypeCAA,
		"PTR":   dns.TypePTR,
	}

	var result []uint16
	for _, t := range strings.Split(types, ",") {
		t = strings.TrimSpace(strings.ToUpper(t))
		if rtype, ok := typeMap[t]; ok {
			result = append(result, rtype)
		}
	}
	return result
}
