package main

import (
	"errors"
	"strings"
)

func (s *Settings) normalize() {
	if s.RoutingMode == "" {
		s.RoutingMode = "auto"
	}
	if s.RefreshMinutes == 0 {
		s.RefreshMinutes = 60
	}
	if s.HealthSeconds == 0 {
		s.HealthSeconds = 15
	}
	if s.FailureThreshold == 0 {
		s.FailureThreshold = 2
	}
}
func (s *Settings) validate() error {
	s.normalize()
	if s.CheckTimeoutMS < 500 || s.CheckTimeoutMS > 10000 {
		return errors.New("妫€鏌ヨ秴鏃跺繀椤诲湪 500鈥?0000 姣")
	}
	if s.RefreshMinutes < 1 || s.RefreshMinutes > 120 {
		return errors.New("鑺傜偣婧愭洿鏂伴棿闅斿繀椤讳负 1鈥?20 鍒嗛挓")
	}
	if s.HealthSeconds < 10 || s.HealthSeconds > 300 || s.FailureThreshold < 1 || s.FailureThreshold > 10 {
		return errors.New("鎺㈡椿闂撮殧涓?10鈥?00 绉掞紝澶辫触闃堝€间负 1鈥?0 娆?)
	}
	s.ForceCountry = strings.ToUpper(strings.TrimSpace(s.ForceCountry))
	switch s.RoutingMode {
	case "auto", "favorites":
	case "fixed_region":
		if !countryCode(s.ForceCountry) {
			return errors.New("鍥哄畾鍥藉妯″紡闇€瑕侀€夋嫨鍥藉")
		}
	case "fixed_ip":
		if s.FixedNodeID == "" {
			return errors.New("鍥哄畾鑺傜偣妯″紡闇€瑕侀€夋嫨鑺傜偣")
		}
	default:
		return errors.New("鏈煡杩炴帴绛栫暐")
	}
	seen := map[string]bool{}
	countries := []string{}
	for _, code := range s.DiscoveryCountries {
		code = strings.ToUpper(strings.TrimSpace(code))
		if !countryCode(code) {
			return errors.New("鍙戠幇鍥藉璇峰～鍐欎袱浣嶅浗瀹朵唬鐮侊紝渚嬪 US,JP,KR")
		}
		if !seen[code] {
			countries = append(countries, code)
			seen[code] = true
		}
	}
	s.DiscoveryCountries = countries
	return nil
}
func countryCode(code string) bool {
	return len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z'
}

// Caller holds a.mu. Explicit row clicks override policy for that first node.
func (a *App) connectionPool(preferred string) []Node {
	pool := []Node{}
	for _, n := range a.nodes {
		if n.Classification != "broadband-candidate" {
			continue
		}
		if n.ID == preferred {
			pool = append(pool, n)
			continue
		}
		switch a.settings.RoutingMode {
		case "auto":
			// A selected country narrows automatic selection and failover.
			// With no country selected, automatic mode keeps the full pool.
			if a.settings.ForceCountry != "" && n.Code != a.settings.ForceCountry {
				continue
			}
		case "fixed_region":
			if n.Code != a.settings.ForceCountry {
				continue
			}
		case "favorites":
			if !a.favorites[n.ID] {
				continue
			}
		case "fixed_ip":
			if n.ID != a.settings.FixedNodeID {
				continue
			}
		}
		pool = append(pool, n)
	}
	return pool
}

