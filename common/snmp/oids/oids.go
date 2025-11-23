package oids

// This package centralizes common SNMP OIDs used throughout the agent.  The
// constants mirror the structure documented in the Host Resources, Printer, and
// Port Monitor MIBs so callers can avoid scattering raw dotted strings.

const (
	// --- System/Host Resources MIB (RFC 2790) ---

	// SysDescr reports a human-readable system description string.
	SysDescr = "1.3.6.1.2.1.1.1.0"
	// SysObjectID contains the authoritative enterprise OID for the device.
	SysObjectID = "1.3.6.1.2.1.1.2.0"
	// HrDeviceDescr points at HOST-RESOURCES-MIB::hrDeviceDescr.1
	HrDeviceDescr = "1.3.6.1.2.1.25.3.2.1.3.1"
)

const (
	// --- Printer MIB (RFC 3805) ---

	// PrtGeneralSerialNumber (prtGeneralSerialNumber.1) is the canonical serial.
	PrtGeneralSerialNumber = "1.3.6.1.2.1.43.5.1.1.17.1"
	// PrtMarkerLifeCount targets prtMarkerLifeCount.1 and is commonly treated as the page counter.
	PrtMarkerLifeCount = "1.3.6.1.2.1.43.10.2.1.4.1"

	// Printer status/error indicators. These align with hrPrinter tables.
	HrPrinterStatus             = "1.3.6.1.2.1.25.3.5.1.1"
	HrPrinterDetectedErrorState = "1.3.6.1.2.1.25.3.5.1.2"

	// Supply/colorant table roots (Printer-MIB::prtMarkerSupplies / prtMarkerColorant).
	PrtMarkerSuppliesEntry   = "1.3.6.1.2.1.43.11.1.1"
	PrtMarkerSuppliesLevel   = "1.3.6.1.2.1.43.11.1.1.9"
	PrtMarkerSuppliesMaxCap  = "1.3.6.1.2.1.43.11.1.1.8"
	PrtMarkerSuppliesClass   = "1.3.6.1.2.1.43.11.1.1.4"
	PrtMarkerSuppliesType    = "1.3.6.1.2.1.43.11.1.1.5"
	PrtMarkerSuppliesDesc    = "1.3.6.1.2.1.43.11.1.1.6"
	PrtMarkerSuppliesColorID = "1.3.6.1.2.1.43.11.1.1.3"

	PrtMarkerColorantValue = "1.3.6.1.2.1.43.12.1.1.4"
)

const (
	// --- Port Monitor (PWG 5100.6) ---

	// PpmPrinterIEEE1284DeviceID provides an IEEE-1284 string via SNMP.
	PpmPrinterIEEE1284DeviceID = "1.3.6.1.4.1.2699.1.2.1.2.1.3"
)
