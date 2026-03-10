package resolver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var fallbackSubdomains = []string{
	"www", "mail", "ftp", "cdn", "api", "app", "dev",
	"staging", "blog", "shop", "store", "admin", "portal",
	"webmail", "smtp", "imap", "pop", "ns1", "ns2",
	"vpn", "remote", "m", "mobile", "autodiscover",
}

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

	// Query discovered subdomains for CNAME records.
	for _, t := range recordTypes {
		if t == dns.TypeCNAME {
			cnameRecords := querySubdomainCNAMEs(domain, server)
			rs.Records = append(rs.Records, cnameRecords...)
			break
		}
	}

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

func discoverSubdomains(domain string) []string {
	// Strip trailing dot for the HTTP query.
	bare := strings.TrimSuffix(domain, ".")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://crt.sh/?q=%25." + bare + "&output=json")
	if err != nil {
		return fallbackSubdomains
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fallbackSubdomains
	}

	var entries []struct {
		NameValue string `json:"name_value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return fallbackSubdomains
	}

	seen := make(map[string]bool)
	var subdomains []string
	for _, e := range entries {
		for _, name := range strings.Split(e.NameValue, "\n") {
			name = strings.TrimSpace(strings.ToLower(name))
			// Skip wildcards and the apex itself.
			if strings.HasPrefix(name, "*.") || name == bare || name == "" {
				continue
			}
			// Only keep direct subdomains of the domain.
			if !strings.HasSuffix(name, "."+bare) {
				continue
			}
			if !seen[name] {
				seen[name] = true
				subdomains = append(subdomains, name)
			}
		}
	}

	// Always merge common subdomains — CT logs miss non-HTTPS entries like mail.
	for _, fb := range fallbackSubdomains {
		full := fb + "." + bare
		if !seen[full] {
			seen[full] = true
			subdomains = append(subdomains, full)
		}
	}
	return subdomains
}

func querySubdomainCNAMEs(domain, server string) []dns.RR {
	subdomains := discoverSubdomains(domain)

	var mu sync.Mutex
	var wg sync.WaitGroup
	var results []dns.RR

	for _, sub := range subdomains {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			fqdn := dns.Fqdn(s)
			records, err := queryRecords(fqdn, dns.TypeCNAME, server)
			if err != nil || len(records) == 0 {
				return
			}
			mu.Lock()
			results = append(results, records...)
			mu.Unlock()
		}(sub)
	}

	wg.Wait()
	return results
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
