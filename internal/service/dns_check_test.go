package service

import (
	"fmt"
	"testing"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// genIPv4 generates a random IPv4 address string
func genIPv4() gopter.Gen {
	return gen.SliceOfN(4, gen.IntRange(1, 254)).Map(func(octets []int) string {
		return fmt.Sprintf("%d.%d.%d.%d", octets[0], octets[1], octets[2], octets[3])
	})
}

// genIPv6 generates a simplified random IPv6 address string
func genIPv6() gopter.Gen {
	return gen.SliceOfN(8, gen.IntRange(0, 0xffff)).Map(func(groups []int) string {
		return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
			groups[0], groups[1], groups[2], groups[3],
			groups[4], groups[5], groups[6], groups[7])
	})
}

// genIPv4List generates a non-empty list of IPv4 addresses
func genIPv4List() gopter.Gen {
	return gen.SliceOfN(3, genIPv4()).SuchThat(func(v []string) bool {
		return len(v) > 0
	})
}

// genIPv6List generates a non-empty list of IPv6 addresses
func genIPv6List() gopter.Gen {
	return gen.SliceOfN(2, genIPv6()).SuchThat(func(v []string) bool {
		return len(v) > 0
	})
}

// Feature: phase6-enhancements, Property 3: DNS 状态判定正确性 — For any domain's A/AAAA record set
// and server IP config, DNS check should return correct status: "matched" when A records contain
// server_ipv4 or AAAA contain server_ipv6; "mismatched" when records exist but don't match;
// "records_only" when both server IPs are empty.
// **Validates: Requirements 2.2, 2.3, 2.4, 2.8**
func TestProperty3_DnsStatusDetermination(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Sub-property: matched when A records contain server_ipv4
	properties.Property("matched when A records contain server_ipv4", prop.ForAll(
		func(aRecords []string, serverIPv4 string) bool {
			// Insert server IP into A records to guarantee a match
			aRecordsWithServer := append([]string{serverIPv4}, aRecords...)
			status := DetermineStatus(aRecordsWithServer, []string{}, serverIPv4, "")
			return status == "matched"
		},
		genIPv4List(),
		genIPv4(),
	))

	// Sub-property: matched when AAAA records contain server_ipv6
	properties.Property("matched when AAAA records contain server_ipv6", prop.ForAll(
		func(aaaaRecords []string, serverIPv6 string) bool {
			aaaaRecordsWithServer := append([]string{serverIPv6}, aaaaRecords...)
			status := DetermineStatus([]string{}, aaaaRecordsWithServer, "", serverIPv6)
			return status == "matched"
		},
		genIPv6List(),
		genIPv6(),
	))

	// Sub-property: mismatched when records exist but don't match
	properties.Property("mismatched when records exist but none match", prop.ForAll(
		func(aRecords []string, aaaaRecords []string, serverIPv4 string, serverIPv6 string) bool {
			// Ensure server IPs are NOT in the record lists
			filteredA := filterOut(aRecords, serverIPv4)
			filteredAAAA := filterOut(aaaaRecords, serverIPv6)
			// Need at least some records
			if len(filteredA) == 0 && len(filteredAAAA) == 0 {
				return true // skip this case
			}
			// Need at least one server IP configured
			if serverIPv4 == "" && serverIPv6 == "" {
				return true // skip — this would be records_only
			}
			status := DetermineStatus(filteredA, filteredAAAA, serverIPv4, serverIPv6)
			return status == "mismatched"
		},
		genIPv4List(),
		genIPv6List(),
		genIPv4(),
		genIPv6(),
	))

	// Sub-property: records_only when both server IPs are empty
	properties.Property("records_only when both server IPs empty", prop.ForAll(
		func(aRecords []string, aaaaRecords []string, domainSuffix int) bool {
			db := setupTestDB(t)
			// No server IPs in settings (default empty)
			lookup := func(domain string) ([]string, []string, error) {
				return aRecords, aaaaRecords, nil
			}
			svc := NewDnsCheckServiceWithLookup(db, lookup)
			result, err := svc.Check(fmt.Sprintf("test-%d.example.com", domainSuffix))
			if err != nil {
				return false
			}
			if len(aRecords) == 0 && len(aaaaRecords) == 0 {
				return result.Status == "no_record"
			}
			return result.Status == "records_only"
		},
		genIPv4List(),
		genIPv6List(),
		gen.IntRange(1, 99999),
	))

	// Sub-property: no_record when DNS lookup fails
	properties.Property("no_record when DNS lookup fails", prop.ForAll(
		func(domainSuffix int) bool {
			db := setupTestDB(t)
			lookup := func(domain string) ([]string, []string, error) {
				return nil, nil, fmt.Errorf("lookup failed: no such host")
			}
			svc := NewDnsCheckServiceWithLookup(db, lookup)
			result, err := svc.Check(fmt.Sprintf("fail-%d.example.com", domainSuffix))
			if err != nil {
				return false
			}
			return result.Status == "no_record" && result.Error != ""
		},
		gen.IntRange(1, 99999),
	))

	// Sub-property: matched via full Check with server IPs in settings
	properties.Property("full Check returns matched when server IP in records", prop.ForAll(
		func(serverIPv4 string, otherIPs []string, domainSuffix int) bool {
			db := setupTestDB(t)
			// Set server_ipv4 in settings
			db.Save(&model.Setting{Key: "server_ipv4", Value: serverIPv4})

			aRecords := append(otherIPs, serverIPv4)
			lookup := func(domain string) ([]string, []string, error) {
				return aRecords, []string{}, nil
			}
			svc := NewDnsCheckServiceWithLookup(db, lookup)
			result, err := svc.Check(fmt.Sprintf("match-%d.example.com", domainSuffix))
			if err != nil {
				return false
			}
			return result.Status == "matched" &&
				result.ExpectedIPv4 == serverIPv4
		},
		genIPv4(),
		genIPv4List(),
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// filterOut removes a specific value from a string slice
func filterOut(slice []string, val string) []string {
	var result []string
	for _, s := range slice {
		if s != val {
			result = append(result, s)
		}
	}
	return result
}
