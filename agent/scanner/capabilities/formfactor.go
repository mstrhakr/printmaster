package capabilities

import (
	"regexp"
	"strings"
)

// FormFactorDetector detects the physical form factor of the device:
// - Desktop (small personal/workgroup printers)
// - Wide Format (large format printers, plotters)
// - Label Printer (industrial label/barcode printers)
// - Production (high-volume production/digital press)
// - Floor-standing Copier (large MFP copiers)
type FormFactorDetector struct{}

func (d *FormFactorDetector) Name() string {
	return "formfactor"
}

func (d *FormFactorDetector) Threshold() float64 {
	return 0.6 // Lower threshold since we want best-match classification
}

// FormFactor constants
const (
	FormFactorDesktop     = "Desktop"
	FormFactorWideFormat  = "Wide Format"
	FormFactorLabelPrint  = "Label Printer"
	FormFactorProduction  = "Production"
	FormFactorFloorCopier = "Floor Copier"
	FormFactorUnknown     = ""
)

// Detect returns a score but the actual form factor is determined by ClassifyFormFactor
func (d *FormFactorDetector) Detect(evidence *DetectionEvidence) float64 {
	if evidence == nil {
		return 0.0
	}
	// This detector exists to register in the system, but actual classification
	// is done by ClassifyFormFactor which returns a string
	return 0.5 // Neutral - classification is done separately
}

// ClassifyFormFactor determines the device form factor based on model name and other evidence
func ClassifyFormFactor(evidence *DetectionEvidence) string {
	if evidence == nil {
		return FormFactorUnknown
	}

	model := strings.ToLower(evidence.Model)
	vendor := strings.ToLower(evidence.Vendor)
	sysDescr := strings.ToLower(evidence.SysDescr)
	combined := model + " " + vendor + " " + sysDescr

	// === Wide Format / Plotter Detection ===
	// HP DesignJet series (plotters/wide format)
	if strings.Contains(combined, "designjet") {
		return FormFactorWideFormat
	}

	// Epson SureColor SC-T/P/F series (wide format inkjets)
	// SC-T = Technical (CAD/GIS)
	// SC-P = Photo
	// SC-F = Dye-sublimation (textile)
	if strings.Contains(model, "surecolor") || strings.Contains(model, "sc-t") ||
		strings.Contains(model, "sc-p") || strings.Contains(model, "sc-f") {
		return FormFactorWideFormat
	}

	// Canon imagePROGRAF (wide format)
	if strings.Contains(combined, "imageprograf") || strings.Contains(model, "ipf-") ||
		strings.Contains(model, "pro-") {
		return FormFactorWideFormat
	}

	// Generic wide format keywords
	wideFormatKeywords := []string{
		"wide format", "large format", "plotter", "36-inch", "44-inch", "60-inch",
		"24-inch", "42-inch", "cad", "gis printer", "poster", "blueprint",
	}
	for _, kw := range wideFormatKeywords {
		if strings.Contains(combined, kw) {
			return FormFactorWideFormat
		}
	}

	// === Label Printer Detection ===
	// Epson ColorWorks (CW-C) - industrial color label printers
	if strings.Contains(model, "cw-c") || strings.Contains(model, "colorworks") {
		return FormFactorLabelPrint
	}

	// Epson TM series (receipt/label printers)
	if regexp.MustCompile(`(?i)\btm-`).MatchString(model) {
		return FormFactorLabelPrint
	}

	// Zebra printers (all are label printers)
	if strings.Contains(vendor, "zebra") || strings.Contains(model, "zebra") {
		return FormFactorLabelPrint
	}

	// SATO printers (label printers)
	if strings.Contains(vendor, "sato") || strings.Contains(model, "sato") {
		return FormFactorLabelPrint
	}

	// TSC printers (label printers)
	if strings.Contains(vendor, "tsc") && !strings.Contains(vendor, "toshiba") {
		return FormFactorLabelPrint
	}

	// Brady, Datamax, Intermec (label printer manufacturers)
	labelVendors := []string{"brady", "datamax", "intermec", "honeywell label", "bixolon"}
	for _, lv := range labelVendors {
		if strings.Contains(combined, lv) {
			return FormFactorLabelPrint
		}
	}

	// Generic label keywords
	labelKeywords := []string{
		"label printer", "barcode", "thermal transfer", "direct thermal",
		"roll printer", "ticket printer", "wristband",
	}
	for _, kw := range labelKeywords {
		if strings.Contains(combined, kw) {
			return FormFactorLabelPrint
		}
	}

	// === Production / Digital Press Detection ===
	// HP Indigo (digital press)
	if strings.Contains(combined, "indigo") {
		return FormFactorProduction
	}

	// Konica Minolta bizhub PRESS
	if strings.Contains(model, "bizhub press") || strings.Contains(model, "accuriopress") {
		return FormFactorProduction
	}

	// Xerox iGen, Versant (production)
	if strings.Contains(model, "igen") || strings.Contains(model, "versant") ||
		strings.Contains(model, "nuvera") {
		return FormFactorProduction
	}

	// Canon imagePRESS
	if strings.Contains(model, "imagepress") {
		return FormFactorProduction
	}

	// Ricoh Pro series (production)
	if regexp.MustCompile(`(?i)pro\s*c?\d{4,5}`).MatchString(model) && strings.Contains(vendor, "ricoh") {
		return FormFactorProduction
	}

	// Generic production keywords
	productionKeywords := []string{"digital press", "production printer", "high volume"}
	for _, kw := range productionKeywords {
		if strings.Contains(combined, kw) {
			return FormFactorProduction
		}
	}

	// === Floor-standing Copier/MFP Detection ===
	// Large MFP series indicators (typically > 30 PPM floor-standing units)
	// Konica Minolta bizhub 4-digit models (e.g., bizhub C658, C754)
	if regexp.MustCompile(`(?i)bizhub\s*c?\d{3,4}`).MatchString(model) {
		// Higher numbers typically = larger floor copiers
		return FormFactorFloorCopier
	}

	// Canon imageRUNNER ADVANCE (floor copiers)
	if strings.Contains(model, "imagerunner advance") || strings.Contains(model, "ir-adv") ||
		strings.Contains(model, "iradv") {
		return FormFactorFloorCopier
	}

	// Ricoh IM/MP series (floor MFPs)
	if regexp.MustCompile(`(?i)(im|mp)\s*c?\d{4,5}`).MatchString(model) && strings.Contains(vendor, "ricoh") {
		return FormFactorFloorCopier
	}

	// Xerox WorkCentre, AltaLink, VersaLink (larger models)
	if strings.Contains(model, "workcentre") || strings.Contains(model, "altalink") {
		return FormFactorFloorCopier
	}

	// Sharp MX series (floor copiers)
	if strings.Contains(model, "mx-") && strings.Contains(vendor, "sharp") {
		return FormFactorFloorCopier
	}

	// Kyocera TASKalfa (floor MFPs)
	if strings.Contains(model, "taskalfa") {
		return FormFactorFloorCopier
	}

	// Toshiba e-STUDIO (floor copiers)
	if strings.Contains(model, "e-studio") || strings.Contains(model, "estudio") {
		return FormFactorFloorCopier
	}

	// === Desktop Detection (default for most standard printers) ===
	// Small workgroup/personal printers

	// Kyocera ECOSYS (typically desktop)
	if strings.Contains(model, "ecosys") {
		return FormFactorDesktop
	}

	// HP LaserJet (most are desktop, except Enterprise MFP floor units)
	if strings.Contains(model, "laserjet") && !strings.Contains(model, "enterprise mfp") {
		return FormFactorDesktop
	}

	// Brother HL, DCP, MFC (typically desktop)
	if regexp.MustCompile(`(?i)(hl|dcp|mfc)-[a-z]?\d{4}`).MatchString(model) {
		return FormFactorDesktop
	}

	// Epson WorkForce, EcoTank (desktop inkjets)
	if strings.Contains(model, "workforce") || strings.Contains(model, "wf-") ||
		strings.Contains(model, "ecotank") || strings.Contains(model, "et-") {
		return FormFactorDesktop
	}

	// Canon imageCLASS (desktop laser)
	if strings.Contains(model, "imageclass") {
		return FormFactorDesktop
	}

	// HP OfficeJet, DeskJet (desktop inkjet)
	if strings.Contains(model, "officejet") || strings.Contains(model, "deskjet") ||
		strings.Contains(model, "envy") {
		return FormFactorDesktop
	}

	// Lexmark (most are desktop unless explicitly MFP series)
	if strings.Contains(vendor, "lexmark") {
		return FormFactorDesktop
	}

	// Default: unknown (let the device type classifier handle it)
	return FormFactorUnknown
}

// IsWideFormat returns true if the device is a wide format/plotter
func IsWideFormat(evidence *DetectionEvidence) bool {
	return ClassifyFormFactor(evidence) == FormFactorWideFormat
}

// IsLabelPrinter returns true if the device is a label/barcode printer
func IsLabelPrinter(evidence *DetectionEvidence) bool {
	return ClassifyFormFactor(evidence) == FormFactorLabelPrint
}

// IsProductionPrinter returns true if the device is a production/digital press
func IsProductionPrinter(evidence *DetectionEvidence) bool {
	return ClassifyFormFactor(evidence) == FormFactorProduction
}

// IsFloorCopier returns true if the device is a floor-standing copier/MFP
func IsFloorCopier(evidence *DetectionEvidence) bool {
	return ClassifyFormFactor(evidence) == FormFactorFloorCopier
}

// IsDesktop returns true if the device is a desktop/workgroup printer
func IsDesktop(evidence *DetectionEvidence) bool {
	return ClassifyFormFactor(evidence) == FormFactorDesktop
}
