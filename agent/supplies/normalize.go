package supplies

import (
	"regexp"
	"strings"
)

// partNumberPattern matches vendor part numbers that end with color codes:
// - Kyocera: TK-8517K, TK-8517C, TK-8517M, TK-8517Y
// - HP: CE400A (K), CE401A (C), etc.
// Format: letters/numbers followed by color suffix (K/C/M/Y)
var partNumberPattern = regexp.MustCompile(`(?i)^(tk|tn|ce|cf|w\d|cb|cc|q\d|c\d)[- ]?\d{3,5}([kcmy])$`)

// monoTonerPattern matches monochrome toner part numbers without color suffix:
// - Kyocera: TK-3182, TK-1172, TK-1152
// - Brother: TN-760, TN-850
// These are always black toner for monochrome printers
var monoTonerPattern = regexp.MustCompile(`(?i)^(tk|tn)[- ]?\d{3,5}$`)

// NormalizeDescription maps a raw supply description to a canonical metric key
// understood by storage and server layers (e.g., "Black Toner" -> "toner_black").
// Returns an empty string if the description cannot be classified.
func NormalizeDescription(desc string) string {
	clean := strings.TrimSpace(desc)
	if clean == "" {
		return ""
	}

	lower := strings.ToLower(clean)

	// Check for vendor part number patterns first (e.g., TK-8517K, TK-8517C)
	// These have the color encoded in the last character
	if result := extractColorFromPartNumber(clean); result != "" {
		return result
	}

	lower = strings.ReplaceAll(lower, "_", " ")
	lower = strings.ReplaceAll(lower, "-", " ")
	lower = strings.ReplaceAll(lower, "\t", " ")
	lower = strings.ReplaceAll(lower, "\n", " ")
	lower = strings.TrimSpace(lower)

	if lower == "" {
		return ""
	}

	isTonerWord := containsAny(lower, []string{"toner", "ink", "cartridge", "developer", "supply"})
	isDrumWord := containsAny(lower, []string{"drum", "imaging", "image", "opc", "photoconductor"})

	// Color-specific detection with drum guardrails (to avoid mapping drums as toner)
	if containsAny(lower, []string{"black", " bk", "bk ", "blk", "negro", "noir", "schwarz", "nero", "má"}) || lower == "k" {
		if isDrumWord && !isTonerWord {
			return "drum_life"
		}
		return "toner_black"
	}
	if containsAny(lower, []string{"cyan", " cy", "cy ", "cyn"}) || lower == "c" {
		if isDrumWord && !isTonerWord {
			return ""
		}
		return "toner_cyan"
	}
	if containsAny(lower, []string{"magenta", " mg", "mg ", " mag", "mag "}) || lower == "m" {
		if isDrumWord && !isTonerWord {
			return ""
		}
		return "toner_magenta"
	}
	if containsAny(lower, []string{"yellow", " yl", "yl ", "yel", "amarillo", "jaune", "gelb", "giallo"}) || lower == "y" {
		if isDrumWord && !isTonerWord {
			return ""
		}
		return "toner_yellow"
	}

	// Non-color consumables
	if isDrumWord {
		return "drum_life"
	}
	if containsAny(lower, []string{"waste", "used"}) {
		return "waste_toner"
	}
	if containsAny(lower, []string{"fuser", "fusing"}) {
		return "fuser_life"
	}
	if containsAny(lower, []string{"transfer", "belt"}) {
		return "transfer_belt"
	}

	return ""
}

// extractColorFromPartNumber checks if the description is a vendor part number
// with color encoded in the suffix (K=black, C=cyan, M=magenta, Y=yellow)
// or a monochrome toner part number (no suffix = black)
func extractColorFromPartNumber(desc string) string {
	// Try the color suffix regex pattern first (e.g., TK-8517K)
	if matches := partNumberPattern.FindStringSubmatch(desc); len(matches) >= 3 {
		colorCode := strings.ToLower(matches[2])
		switch colorCode {
		case "k":
			return "toner_black"
		case "c":
			return "toner_cyan"
		case "m":
			return "toner_magenta"
		case "y":
			return "toner_yellow"
		}
	}

	// Try monochrome toner pattern (e.g., TK-3182, TN-760)
	// These are black toner for monochrome printers
	if monoTonerPattern.MatchString(desc) {
		return "toner_black"
	}

	// Fallback: check for common patterns not caught by regex
	// e.g., "Supply TK-8517K" or similar prefixed variants
	lower := strings.ToLower(desc)
	// Remove common prefixes
	lower = strings.TrimPrefix(lower, "supply ")
	lower = strings.TrimSpace(lower)

	// Check if it looks like a part number ending in a color code
	if len(lower) >= 4 && strings.ContainsAny(lower, "0123456789") {
		lastChar := lower[len(lower)-1]
		// Make sure the second-to-last is a digit (part of the number)
		if len(lower) >= 2 && lower[len(lower)-2] >= '0' && lower[len(lower)-2] <= '9' {
			switch lastChar {
			case 'k':
				return "toner_black"
			case 'c':
				return "toner_cyan"
			case 'm':
				return "toner_magenta"
			case 'y':
				return "toner_yellow"
			}
		}
	}

	return ""
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
