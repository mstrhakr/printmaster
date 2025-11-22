package supplies

import "strings"

// NormalizeDescription maps a raw supply description to a canonical metric key
// understood by storage and server layers (e.g., "Black Toner" -> "toner_black").
// Returns an empty string if the description cannot be classified.
func NormalizeDescription(desc string) string {
	clean := strings.TrimSpace(desc)
	if clean == "" {
		return ""
	}

	lower := strings.ToLower(clean)
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
	if containsAny(lower, []string{"magenta", " mg", "mg ", "mag"}) || lower == "m" {
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

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
