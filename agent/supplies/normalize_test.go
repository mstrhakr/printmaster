package supplies

import "testing"

func TestNormalizeDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Empty/whitespace inputs
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"tabs and newlines", "\t\n  ", ""},

		// Black toner variants
		{"black toner", "Black Toner", "toner_black"},
		{"black ink", "Black Ink", "toner_black"},
		{"bk cartridge", "BK Cartridge", "toner_black"},
		{"toner k", "Toner K", ""}, // "k" only matches when lower == "k"
		{"negro", "Negro", "toner_black"},
		{"noir", "Noir", "toner_black"},
		{"schwarz", "Schwarz", "toner_black"},
		{"nero", "Nero", "toner_black"},
		{"blk developer", "Blk Developer", "toner_black"},

		// Cyan toner variants
		{"cyan toner", "Cyan Toner", "toner_cyan"},
		{"cyan ink", "Cyan Ink", "toner_cyan"},
		{"cy cartridge", "CY Cartridge", "toner_cyan"},
		{"cyn supply", "Cyn Supply", "toner_cyan"},

		// Magenta toner variants
		{"magenta toner", "Magenta Toner", "toner_magenta"},
		{"magenta ink", "Magenta Ink", "toner_magenta"},
		{"mg cartridge", "MG Cartridge", "toner_magenta"},
		{"mag supply", "Mag Supply", "toner_magenta"},

		// Yellow toner variants
		{"yellow toner", "Yellow Toner", "toner_yellow"},
		{"yellow ink", "Yellow Ink", "toner_yellow"},
		{"yl cartridge", "YL Cartridge", "toner_yellow"},
		{"yel supply", "Yel Supply", "toner_yellow"},
		{"amarillo", "Amarillo", "toner_yellow"},
		{"jaune", "Jaune", "toner_yellow"},
		{"gelb", "Gelb", "toner_yellow"},
		{"giallo", "Giallo", "toner_yellow"},

		// Drum units
		{"drum unit", "Drum Unit", "drum_life"},
		{"imaging unit", "Imaging Unit", "drum_life"}, // fixed: no longer false-positive on "mag" in "imaging"
		{"image drum", "Image Drum", "drum_life"},     // "image" matches
		{"opc drum", "OPC Drum", "drum_life"},
		{"photoconductor", "Photoconductor", "drum_life"},
		{"black drum", "Black Drum", "drum_life"},

		// Other consumables
		{"waste toner", "Waste Toner", "waste_toner"},
		{"used toner", "Used Toner Container", "waste_toner"},
		{"fuser unit", "Fuser Unit", "fuser_life"},
		{"fusing assembly", "Fusing Assembly", "fuser_life"},
		{"transfer belt", "Transfer Belt", "transfer_belt"},
		{"belt unit", "Belt Unit", "transfer_belt"},

		// Mixed case and special characters
		{"mixed case", "CYAN_TONER", "toner_cyan"},
		{"dashes", "yellow-ink", "toner_yellow"},
		{"tabs", "black\ttoner", "toner_black"},

		// Unknown/unmapped
		{"unknown supply", "Paper Tray", ""},
		{"random text", "Something Else", ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeDescription(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeDescription(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		haystack string
		needles  []string
		want     bool
	}{
		{"black toner", []string{"black", "cyan"}, true},
		{"magenta ink", []string{"black", "cyan"}, false},
		{"", []string{"anything"}, false},
		{"something", []string{}, false},
		{"hello world", []string{"world"}, true},
	}

	for _, tc := range tests {
		got := containsAny(tc.haystack, tc.needles)
		if got != tc.want {
			t.Errorf("containsAny(%q, %v) = %v, want %v", tc.haystack, tc.needles, got, tc.want)
		}
	}
}
