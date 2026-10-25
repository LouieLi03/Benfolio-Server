// Package position validates portable content locations without loading documents.
package position

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
)

func ValidFraction(value *float64) bool {
	return value == nil || !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 1
}

func ValidEPUBLocator(raw []byte) bool {
	// Preserve the complete client locator (CFI, fragments, quote and extensions)
	// without converting it into device-dependent screen/page coordinates.
	object, err := jsonvalue.Object(raw)
	if err != nil {
		return false
	}
	var href, mediaType string
	if json.Unmarshal(object["href"], &href) != nil || json.Unmarshal(object["type"], &mediaType) != nil ||
		!ValidEPUBHref(href) || mediaType == "" || len(mediaType) > 256 {
		return false
	}
	if rawLocations, exists := object["locations"]; exists && string(rawLocations) != "null" {
		positions, err := jsonvalue.Object(rawLocations)
		if err != nil {
			return false
		}
		for _, key := range []string{"progression", "totalProgression"} {
			if raw, exists := positions[key]; exists {
				var fraction *float64
				if json.Unmarshal(raw, &fraction) != nil || !ValidFraction(fraction) {
					return false
				}
			}
		}
		if raw, exists := positions["position"]; exists {
			var position *int64
			if json.Unmarshal(raw, &position) != nil || position != nil && (*position < 1 || *position > domain.MaxMutationRevision) {
				return false
			}
		}
	}
	return true
}

// ValidEPUBHref accepts a content URI within a publication, never a remote URL.
func ValidEPUBHref(href string) bool {
	if href == "" || len(href) > 4096 || strings.ContainsAny(href, "\\\x00\r\n") {
		return false
	}
	u, err := url.Parse(href)
	return err == nil && !u.IsAbs() && u.Host == "" && u.Path != ""
}
