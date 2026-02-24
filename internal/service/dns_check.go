package service

import (
	"net"

	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// DnsCheckResult holds the result of a DNS check for a domain
type DnsCheckResult struct {
	Status       string   `json:"status"`        // "matched", "mismatched", "no_record", "records_only"
	ARecords     []string `json:"a_records"`      // IPv4 addresses
	AAAARecords  []string `json:"aaaa_records"`   // IPv6 addresses
	ExpectedIPv4 string   `json:"expected_ipv4"`  // server_ipv4 from Settings
	ExpectedIPv6 string   `json:"expected_ipv6"`  // server_ipv6 from Settings
	Error        string   `json:"error,omitempty"` // error info when status is no_record
}

// DnsLookupFunc abstracts DNS lookup for testability.
// It takes a domain and returns A records, AAAA records, and an error.
type DnsLookupFunc func(domain string) (aRecords []string, aaaaRecords []string, err error)

// DefaultDnsLookup performs real DNS lookups using net.LookupIP.
func DefaultDnsLookup(domain string) ([]string, []string, error) {
	ips, err := net.LookupIP(domain)
	if err != nil {
		return nil, nil, err
	}

	var aRecords, aaaaRecords []string
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			aRecords = append(aRecords, ip4.String())
		} else {
			aaaaRecords = append(aaaaRecords, ip.String())
		}
	}
	return aRecords, aaaaRecords, nil
}

// DnsCheckService handles DNS resolution checking
type DnsCheckService struct {
	db     *gorm.DB
	lookup DnsLookupFunc
}

// NewDnsCheckService creates a new DnsCheckService with the default DNS lookup
func NewDnsCheckService(db *gorm.DB) *DnsCheckService {
	return &DnsCheckService{db: db, lookup: DefaultDnsLookup}
}

// NewDnsCheckServiceWithLookup creates a DnsCheckService with a custom lookup function (for testing)
func NewDnsCheckServiceWithLookup(db *gorm.DB, lookup DnsLookupFunc) *DnsCheckService {
	return &DnsCheckService{db: db, lookup: lookup}
}

// Check performs a DNS check for the given domain
func (s *DnsCheckService) Check(domain string) (*DnsCheckResult, error) {
	// Read server IPs from Settings
	serverIPv4 := s.getSetting("server_ipv4")
	serverIPv6 := s.getSetting("server_ipv6")

	result := &DnsCheckResult{
		ExpectedIPv4: serverIPv4,
		ExpectedIPv6: serverIPv6,
	}

	// Perform DNS lookup
	aRecords, aaaaRecords, err := s.lookup(domain)
	if err != nil {
		result.Status = "no_record"
		result.ARecords = []string{}
		result.AAAARecords = []string{}
		result.Error = err.Error()
		return result, nil
	}

	if aRecords == nil {
		aRecords = []string{}
	}
	if aaaaRecords == nil {
		aaaaRecords = []string{}
	}
	result.ARecords = aRecords
	result.AAAARecords = aaaaRecords

	// No records at all
	if len(aRecords) == 0 && len(aaaaRecords) == 0 {
		result.Status = "no_record"
		result.Error = "no A or AAAA records found"
		return result, nil
	}

	// Both server IPs empty → records_only
	if serverIPv4 == "" && serverIPv6 == "" {
		result.Status = "records_only"
		return result, nil
	}

	// Check for match
	result.Status = DetermineStatus(aRecords, aaaaRecords, serverIPv4, serverIPv6)
	return result, nil
}

// DetermineStatus is the pure status determination logic, exported for testing.
// It assumes records exist and at least one server IP is configured.
func DetermineStatus(aRecords, aaaaRecords []string, serverIPv4, serverIPv6 string) string {
	if serverIPv4 != "" {
		for _, a := range aRecords {
			if a == serverIPv4 {
				return "matched"
			}
		}
	}
	if serverIPv6 != "" {
		for _, aaaa := range aaaaRecords {
			if aaaa == serverIPv6 {
				return "matched"
			}
		}
	}
	return "mismatched"
}

func (s *DnsCheckService) getSetting(key string) string {
	var setting model.Setting
	if s.db.Where("key = ?", key).First(&setting).Error == nil {
		return setting.Value
	}
	return ""
}
