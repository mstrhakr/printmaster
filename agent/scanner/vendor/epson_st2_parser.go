package vendor

import (
	"encoding/binary"
	"fmt"
	"strings"

	"printmaster/common/logger"
)

// ST2Status holds the parsed status from an Epson ST2 response frame.
type ST2Status struct {
	// Printer state
	StatusCode    int
	StatusText    string
	Ready         bool
	ErrorCode     string
	WarningCodes  []string
	SelfPrintCode string

	// Ink levels: map of color name -> level (0-100, -1 for unknown)
	InkLevels map[string]int

	// Maintenance box status: map of box number -> status string
	MaintenanceBoxes map[int]string

	// Raw data for debugging
	RawFields map[int][]byte
}

// ST2 frame field types (from epson_print_conf.py status_parser)
const (
	st2FieldStatus           = 0x01 // Printer status code
	st2FieldError            = 0x02 // Error code
	st2FieldSelfPrint        = 0x03 // Self print code
	st2FieldWarning          = 0x04 // Warning codes
	st2FieldPaperPath        = 0x06 // Paper path info
	st2FieldPaperError       = 0x07 // Paper mismatch error
	st2FieldCleaningTime     = 0x0c // Cleaning time info
	st2FieldTanks            = 0x0d // Maintenance tanks
	st2FieldReplaceCartridge = 0x0e // Replace cartridge info
	st2FieldInkInfo          = 0x0f // Ink levels
	st2FieldLoadingPath      = 0x10 // Loading path info
	st2FieldCancelCode       = 0x13 // Cancel code
	st2FieldCutter           = 0x14 // Cutter info
	st2FieldTrayOpen         = 0x18 // Stacker/tray open status
	st2FieldJobName          = 0x19 // Current job name
	st2FieldTemperature      = 0x1c // Temperature info
	st2FieldSerial           = 0x1f // Serial number
	st2FieldPaperJam         = 0x35 // Paper jam error
	st2FieldPaperCount       = 0x36 // Paper count info
	st2FieldMaintenanceBox   = 0x37 // Maintenance box status
	st2FieldInterfaceStatus  = 0x3d // Printer interface status
	st2FieldSerialInfo       = 0x40 // Serial number info
	st2FieldInkReplacement   = 0x45 // Ink replacement counter
	st2FieldMaintBoxReplace  = 0x46 // Maintenance box replacement counter
)

// Ink color ID to name mapping (cartridge type from ST2 response)
var inkColorIDs = map[byte]string{
	0x01: "Black",
	0x03: "Cyan",
	0x04: "Magenta",
	0x05: "Yellow",
	0x06: "Light Cyan",
	0x07: "Light Magenta",
	0x0a: "Light Black",
	0x0b: "Matte Black",
	0x0f: "Light Light Black",
	0x10: "Orange",
	0x11: "Green",
}

// Secondary ink color mapping (ink color field)
var inkColorNames = map[byte]string{
	0x00: "Black",
	0x01: "Cyan",
	0x02: "Magenta",
	0x03: "Yellow",
	0x04: "Light Cyan",
	0x05: "Light Magenta",
	0x06: "Dark Yellow",
	0x07: "Grey",
	0x08: "Light Black",
	0x09: "Red",
	0x0A: "Blue",
	0x0B: "Gloss Optimizer",
	0x0C: "Light Grey",
	0x0D: "Orange",
}

// Printer status codes
var statusCodes = map[byte]string{
	0x00: "Error",
	0x01: "Self Printing",
	0x02: "Busy",
	0x03: "Waiting",
	0x04: "Idle",
	0x05: "Paused",
	0x07: "Cleaning",
	0x08: "Not Initialized",
	0x0a: "Shutdown",
	0x0f: "Nozzle Check",
	0x11: "Charging",
}

// Error codes
var errorCodes = map[byte]string{
	// 0x00: No error (not in map, empty string returned)
	0x01: "Other interface selected",
	0x02: "Cover open",
	0x03: "Fatal error",
	0x04: "Paper jam",
	0x05: "Ink out",
	0x06: "Paper out",
	0x0c: "Paper size/type/path error",
	0x10: "Waste ink pad overflow",
	0x11: "Wait return from tear-off",
	0x12: "Double feed",
	0x1a: "Cartridge cover open",
	0x1c: "Cutter error (fatal)",
	0x1d: "Cutter jam (recoverable)",
	0x22: "Maintenance cartridge missing",
	0x25: "Rear cover open",
	0x29: "CD-R tray out",
	0x2a: "Memory card error",
	0x2B: "Tray cover open",
	0x2C: "Ink cartridge overflow",
	0x2F: "Battery voltage error",
	0x30: "Battery temperature error",
	0x31: "Battery empty",
	0x33: "Initial filling impossible",
	0x36: "Maintenance cartridge cover open",
	0x37: "Scanner/front cover open",
	0x41: "Maintenance request",
	0x47: "Printing disabled",
	0x4a: "Maintenance box near end",
	0x4b: "Driver mismatch",
}

// Warning codes
var warningCodes = map[byte]string{
	0x10: "Ink low (Black or Yellow)",
	0x11: "Ink low (Magenta)",
	0x12: "Ink low (Yellow or Cyan)",
	0x13: "Ink low (Cyan or Matte Black)",
	0x14: "Ink low (Photo Black)",
	0x15: "Ink low (Red)",
	0x16: "Ink low (Blue)",
	0x17: "Ink low (Gloss Optimizer)",
	0x44: "Black print mode",
	0x51: "Cleaning disabled (Cyan)",
	0x52: "Cleaning disabled (Magenta)",
	0x53: "Cleaning disabled (Yellow)",
	0x54: "Cleaning disabled (Black)",
}

// ParseST2Response parses an Epson ST2 status response frame.
// The response format is: \x00@BDC ST2\r\n<len_lo><len_hi><TLV data...>
// Returns parsed status or error if format is invalid.
func ParseST2Response(data []byte) (*ST2Status, error) {
	if len(data) < 16 {
		if logger.Global != nil {
			logger.Global.TraceTag("epson_st2", "ST2 parse: response too short", "len", len(data))
		}
		return nil, fmt.Errorf("ST2 response too short: %d bytes", len(data))
	}

	// Find the BDC ST2 header - it may not be at the start
	header := []byte("BDC ST2\r\n")
	headerIdx := findBytes(data, header)
	if headerIdx < 0 {
		if logger.Global != nil {
			logger.Global.TraceTag("epson_st2", "ST2 parse: header not found", "len", len(data))
		}
		return nil, fmt.Errorf("ST2 header not found (expected 'BDC ST2\\r\\n')")
	}

	// Skip to after header
	data = data[headerIdx+len(header):]
	if len(data) < 2 {
		if logger.Global != nil {
			logger.Global.TraceTag("epson_st2", "ST2 parse: truncated after header")
		}
		return nil, fmt.Errorf("ST2 response truncated after header")
	}

	// Read payload length (little-endian 16-bit)
	payloadLen := int(binary.LittleEndian.Uint16(data[:2]))
	data = data[2:]

	if len(data) < payloadLen {
		if logger.Global != nil {
			logger.Global.TraceTag("epson_st2", "ST2 parse: payload truncated", "expected", payloadLen, "got", len(data))
		}
		return nil, fmt.Errorf("ST2 payload truncated: expected %d, got %d", payloadLen, len(data))
	}

	if logger.Global != nil {
		logger.Global.TraceTag("epson_st2", "ST2 parse: payload", "payload_len", payloadLen)
	}

	// Trim to payload
	buf := data[:payloadLen]

	status := &ST2Status{
		InkLevels:        make(map[string]int),
		MaintenanceBoxes: make(map[int]string),
		WarningCodes:     []string{},
		RawFields:        make(map[int][]byte),
	}

	// Parse TLV fields
	for len(buf) >= 2 {
		fieldType := buf[0]
		fieldLen := int(buf[1])
		buf = buf[2:]

		if len(buf) < fieldLen {
			break // Truncated field
		}

		item := buf[:fieldLen]
		buf = buf[fieldLen:]

		// Store raw for debugging
		status.RawFields[int(fieldType)] = item

		// Parse known fields
		switch fieldType {
		case st2FieldStatus:
			if len(item) >= 1 {
				status.StatusCode = int(item[0])
				if text, ok := statusCodes[item[0]]; ok {
					status.StatusText = text
				} else {
					status.StatusText = fmt.Sprintf("Unknown (%d)", item[0])
				}
				status.Ready = (item[0] == 0x03 || item[0] == 0x04) // Waiting or Idle
			}

		case st2FieldError:
			if len(item) >= 1 {
				if text, ok := errorCodes[item[0]]; ok {
					status.ErrorCode = text
				} else if item[0] != 0 {
					status.ErrorCode = fmt.Sprintf("Unknown error (%d)", item[0])
				}
			}

		case st2FieldWarning:
			for _, w := range item {
				if text, ok := warningCodes[w]; ok {
					status.WarningCodes = append(status.WarningCodes, text)
				} else if w != 0 {
					status.WarningCodes = append(status.WarningCodes, fmt.Sprintf("Unknown warning (%d)", w))
				}
			}

		case st2FieldInkInfo:
			// Format: <colorlen> then repeating <colour><ink_color><level>
			if len(item) < 1 {
				continue
			}
			colorLen := int(item[0])
			if colorLen < 3 {
				colorLen = 3 // Minimum: colour, ink_color, level
			}
			offset := 1
			for offset+colorLen <= len(item) {
				colour := item[offset]
				inkColor := item[offset+1] // Secondary color ID
				level := int(item[offset+2])

				// Map color ID to name - try primary ID first, then secondary
				colorName := "Unknown"
				if name, ok := inkColorIDs[colour]; ok {
					colorName = name
				} else if name, ok := inkColorNames[inkColor]; ok {
					colorName = name
				}

				// Normalize level (0-100 range, or -1 for unknown)
				if level > 100 {
					level = -1 // Unknown/invalid
				}

				status.InkLevels[colorName] = level
				offset += colorLen
			}

		case st2FieldMaintenanceBox:
			// Format: <num_bytes> then repeating status bytes
			if len(item) < 1 {
				continue
			}
			numBytes := int(item[0])
			if numBytes < 1 || numBytes > 2 {
				continue
			}
			boxNum := 1
			for i := 1; i < len(item); i += numBytes {
				boxStatus := item[i]
				var statusText string
				switch boxStatus {
				case 0:
					statusText = "OK"
				case 1:
					statusText = "Near Full"
				case 2:
					statusText = "Full"
				default:
					statusText = fmt.Sprintf("Unknown (%d)", boxStatus)
				}
				status.MaintenanceBoxes[boxNum] = statusText
				boxNum++
			}

		case st2FieldSelfPrint:
			if len(item) >= 1 && item[0] == 0 {
				status.SelfPrintCode = "Nozzle test printing"
			}
		}
	}

	return status, nil
}

// ToMetrics converts ST2Status to a map suitable for device metrics.
func (s *ST2Status) ToMetrics() map[string]interface{} {
	result := make(map[string]interface{})

	// Printer status
	if s.StatusText != "" {
		result["epson_status"] = s.StatusText
	}
	result["epson_ready"] = s.Ready

	if s.ErrorCode != "" {
		result["epson_error"] = s.ErrorCode
	}

	if len(s.WarningCodes) > 0 {
		result["epson_warnings"] = strings.Join(s.WarningCodes, "; ")
	}

	// Ink levels - normalize to our supply naming convention
	for color, level := range s.InkLevels {
		if level < 0 {
			continue // Skip unknown levels
		}
		// Create normalized key: ink_black, ink_cyan, etc.
		key := "ink_" + strings.ToLower(strings.ReplaceAll(color, " ", "_"))
		result[key] = float64(level)
	}

	// Maintenance box status
	for boxNum, status := range s.MaintenanceBoxes {
		key := fmt.Sprintf("maintenance_box_%d", boxNum)
		result[key] = status

		// Also provide numeric waste level estimate
		wasteKey := fmt.Sprintf("waste_box_%d_status", boxNum)
		switch status {
		case "OK":
			result[wasteKey] = 0
		case "Near Full":
			result[wasteKey] = 80
		case "Full":
			result[wasteKey] = 100
		}
	}

	return result
}

// findBytes finds the index of needle in haystack, or -1 if not found.
func findBytes(haystack, needle []byte) int {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		found := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				found = false
				break
			}
		}
		if found {
			return i
		}
	}
	return -1
}
