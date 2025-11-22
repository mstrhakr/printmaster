package vendor

import (
	"fmt"
	"strconv"
	"strings"
)

const epsonRemoteBaseOID = "1.3.6.1.4.1.1248.1.2.2.44.1.1.2.1"

// EpsonRemoteCommand describes a remote-mode command understood by Epson devices.
type EpsonRemoteCommand struct {
	Name        string
	Code        [2]byte
	BasePayload []byte
}

var (
	// EpsonRemoteDeviceIDCommand fetches IEEE-1284 style key/value text (command "di").
	EpsonRemoteDeviceIDCommand = EpsonRemoteCommand{
		Name:        "device_id",
		Code:        [2]byte{'d', 'i'},
		BasePayload: []byte{0x01},
	}
	// EpsonRemoteStatusCommand fetches the ST2 status frame (command "st").
	EpsonRemoteStatusCommand = EpsonRemoteCommand{
		Name:        "status",
		Code:        [2]byte{'s', 't'},
		BasePayload: []byte{0x01},
	}
	// EpsonRemoteInkActuatorCommand returns installed cartridge SKUs (command "ia").
	EpsonRemoteInkActuatorCommand = EpsonRemoteCommand{
		Name:        "ink_actuators",
		Code:        [2]byte{'i', 'a'},
		BasePayload: []byte{0x00},
	}
	// EpsonRemoteInkSlotCommand inspects a specific slot (command "ii").
	EpsonRemoteInkSlotCommand = EpsonRemoteCommand{
		Name:        "ink_slot",
		Code:        [2]byte{'i', 'i'},
		BasePayload: []byte{0x01},
	}
	// EpsonRemoteEEPROMReadCommand issues raw EEPROM reads (command "||").
	EpsonRemoteEEPROMReadCommand = EpsonRemoteCommand{
		Name:        "eeprom_read",
		Code:        [2]byte{'|', '|'},
		BasePayload: nil,
	}
)

// BuildEpsonRemoteOID constructs the SNMP OID for a remote-mode request.
// The payload length is encoded little-endian immediately after the command bytes.
func BuildEpsonRemoteOID(cmd EpsonRemoteCommand, dynamicPayload []byte) (string, error) {
	payload := append([]byte{}, cmd.BasePayload...)
	payload = append(payload, dynamicPayload...)

	if len(payload) > 0xFFFF {
		return "", fmt.Errorf("payload too large for remote command %s", cmd.Name)
	}

	suffix := []string{
		strconv.Itoa(int(cmd.Code[0])),
		strconv.Itoa(int(cmd.Code[1])),
		strconv.Itoa(len(payload) & 0xFF),
		strconv.Itoa((len(payload) >> 8) & 0xFF),
	}

	for _, b := range payload {
		suffix = append(suffix, strconv.Itoa(int(b)))
	}

	return epsonRemoteBaseOID + "." + strings.Join(suffix, "."), nil
}

// BuildEpsonInkSlotOID returns the command OID for inspecting a specific ink slot.
func BuildEpsonInkSlotOID(slot byte) (string, error) {
	return BuildEpsonRemoteOID(EpsonRemoteInkSlotCommand, []byte{slot})
}

// BuildEpsonEEPROMReadOID encodes a raw EEPROM operation payload for the "||" command.
func BuildEpsonEEPROMReadOID(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", fmt.Errorf("eeprom payload required")
	}
	return BuildEpsonRemoteOID(EpsonRemoteEEPROMReadCommand, payload)
}
