package capabilities

import "testing"

func TestClassifyFormFactor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		evidence *DetectionEvidence
		want     string
	}{
		// Wide Format / Plotters
		{
			name: "HP DesignJet T730",
			evidence: &DetectionEvidence{
				Model:  "HP DesignJet T730",
				Vendor: "HP",
			},
			want: FormFactorWideFormat,
		},
		{
			name: "Epson SureColor SC-T5170",
			evidence: &DetectionEvidence{
				Model:  "EPSON SureColor SC-T5170",
				Vendor: "Epson",
			},
			want: FormFactorWideFormat,
		},
		{
			name: "Canon imagePROGRAF iPF670",
			evidence: &DetectionEvidence{
				Model:  "Canon imagePROGRAF iPF670",
				Vendor: "Canon",
			},
			want: FormFactorWideFormat,
		},
		{
			name: "generic wide format",
			evidence: &DetectionEvidence{
				Model:    "Generic Plotter",
				SysDescr: "36-inch wide format printer",
			},
			want: FormFactorWideFormat,
		},

		// Label Printers
		{
			name: "Epson ColorWorks CW-C6500Au",
			evidence: &DetectionEvidence{
				Model:  "EPSON CW-C6500Au",
				Vendor: "Epson",
			},
			want: FormFactorLabelPrint,
		},
		{
			name: "Epson TM-C3500",
			evidence: &DetectionEvidence{
				Model:  "Epson TM-C3500",
				Vendor: "Epson",
			},
			want: FormFactorLabelPrint,
		},
		{
			name: "Zebra ZT410",
			evidence: &DetectionEvidence{
				Model:  "Zebra ZT410",
				Vendor: "Zebra Technologies",
			},
			want: FormFactorLabelPrint,
		},
		{
			name: "SATO label printer",
			evidence: &DetectionEvidence{
				Model:  "SATO CL4NX",
				Vendor: "SATO",
			},
			want: FormFactorLabelPrint,
		},
		{
			name: "generic barcode printer",
			evidence: &DetectionEvidence{
				Model:    "Industrial Printer",
				SysDescr: "Barcode label printer with thermal transfer",
			},
			want: FormFactorLabelPrint,
		},

		// Production / Digital Press
		{
			name: "HP Indigo 7K",
			evidence: &DetectionEvidence{
				Model:  "HP Indigo 7K",
				Vendor: "HP",
			},
			want: FormFactorProduction,
		},
		{
			name: "Konica Minolta AccurioPress",
			evidence: &DetectionEvidence{
				Model:  "AccurioPress C6100",
				Vendor: "Konica Minolta",
			},
			want: FormFactorProduction,
		},
		{
			name: "Xerox Versant",
			evidence: &DetectionEvidence{
				Model:  "Xerox Versant 180",
				Vendor: "Xerox",
			},
			want: FormFactorProduction,
		},
		{
			name: "Canon imagePRESS",
			evidence: &DetectionEvidence{
				Model:  "Canon imagePRESS C10000VP",
				Vendor: "Canon",
			},
			want: FormFactorProduction,
		},

		// Floor Copiers / Large MFPs
		{
			name: "Konica Minolta bizhub C658",
			evidence: &DetectionEvidence{
				Model:  "bizhub C658",
				Vendor: "Konica Minolta",
			},
			want: FormFactorFloorCopier,
		},
		{
			name: "Canon imageRUNNER ADVANCE",
			evidence: &DetectionEvidence{
				Model:  "imageRUNNER ADVANCE C5540i",
				Vendor: "Canon",
			},
			want: FormFactorFloorCopier,
		},
		{
			name: "Xerox WorkCentre",
			evidence: &DetectionEvidence{
				Model:  "Xerox WorkCentre 7855",
				Vendor: "Xerox",
			},
			want: FormFactorFloorCopier,
		},
		{
			name: "Sharp MX series",
			evidence: &DetectionEvidence{
				Model:  "MX-5070N",
				Vendor: "Sharp",
			},
			want: FormFactorFloorCopier,
		},
		{
			name: "Kyocera TASKalfa",
			evidence: &DetectionEvidence{
				Model:  "TASKalfa 5053ci",
				Vendor: "Kyocera",
			},
			want: FormFactorFloorCopier,
		},
		{
			name: "Toshiba e-STUDIO",
			evidence: &DetectionEvidence{
				Model:  "e-STUDIO5015AC",
				Vendor: "Toshiba",
			},
			want: FormFactorFloorCopier,
		},

		// Desktop Printers
		{
			name: "Kyocera ECOSYS P2040dw",
			evidence: &DetectionEvidence{
				Model:  "ECOSYS P2040dw",
				Vendor: "Kyocera",
			},
			want: FormFactorDesktop,
		},
		{
			name: "HP LaserJet Pro M404dn",
			evidence: &DetectionEvidence{
				Model:  "HP LaserJet Pro M404dn",
				Vendor: "HP",
			},
			want: FormFactorDesktop,
		},
		{
			name: "Brother MFC-L8610CDW",
			evidence: &DetectionEvidence{
				Model:  "Brother MFC-L8610CDW",
				Vendor: "Brother",
			},
			want: FormFactorDesktop,
		},
		{
			name: "Epson WorkForce Pro WF-4740",
			evidence: &DetectionEvidence{
				Model:  "WorkForce Pro WF-4740",
				Vendor: "Epson",
			},
			want: FormFactorDesktop,
		},
		{
			name: "Canon imageCLASS MF743Cdw",
			evidence: &DetectionEvidence{
				Model:  "Canon imageCLASS MF743Cdw",
				Vendor: "Canon",
			},
			want: FormFactorDesktop,
		},
		{
			name: "HP OfficeJet Pro",
			evidence: &DetectionEvidence{
				Model:  "HP OfficeJet Pro 9025",
				Vendor: "HP",
			},
			want: FormFactorDesktop,
		},

		// Unknown / Edge cases
		{
			name:     "nil evidence",
			evidence: nil,
			want:     FormFactorUnknown,
		},
		{
			name: "empty model",
			evidence: &DetectionEvidence{
				Model:  "",
				Vendor: "",
			},
			want: FormFactorUnknown,
		},
		{
			name: "unknown device",
			evidence: &DetectionEvidence{
				Model:  "Generic Printer Model X1",
				Vendor: "Unknown Vendor",
			},
			want: FormFactorUnknown,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyFormFactor(tc.evidence)
			if got != tc.want {
				t.Errorf("ClassifyFormFactor() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsWideFormat(t *testing.T) {
	t.Parallel()

	evidence := &DetectionEvidence{
		Model:  "HP DesignJet T730",
		Vendor: "HP",
	}

	if !IsWideFormat(evidence) {
		t.Errorf("IsWideFormat() should return true for DesignJet")
	}

	nonWideFormat := &DetectionEvidence{
		Model:  "HP LaserJet Pro M404dn",
		Vendor: "HP",
	}

	if IsWideFormat(nonWideFormat) {
		t.Errorf("IsWideFormat() should return false for LaserJet")
	}
}

func TestIsLabelPrinter(t *testing.T) {
	t.Parallel()

	evidence := &DetectionEvidence{
		Model:  "EPSON CW-C6500Au",
		Vendor: "Epson",
	}

	if !IsLabelPrinter(evidence) {
		t.Errorf("IsLabelPrinter() should return true for CW-C6500Au")
	}

	nonLabel := &DetectionEvidence{
		Model:  "EPSON WorkForce WF-4740",
		Vendor: "Epson",
	}

	if IsLabelPrinter(nonLabel) {
		t.Errorf("IsLabelPrinter() should return false for WorkForce")
	}
}

func TestClassifyDeviceTypeWithFormFactor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		caps     DeviceCapabilities
		wantType string
	}{
		{
			name: "Color Wide Format",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsColor:    true,
				FormFactor: FormFactorWideFormat,
			},
			wantType: "Color Wide Format",
		},
		{
			name: "Mono Wide Format",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsMono:     true,
				FormFactor: FormFactorWideFormat,
			},
			wantType: "Wide Format",
		},
		{
			name: "Color Label Printer",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsColor:    true,
				FormFactor: FormFactorLabelPrint,
			},
			wantType: "Color Label Printer",
		},
		{
			name: "Mono Label Printer",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsMono:     true,
				FormFactor: FormFactorLabelPrint,
			},
			wantType: "Label Printer",
		},
		{
			name: "Color Production",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsColor:    true,
				FormFactor: FormFactorProduction,
			},
			wantType: "Color Production",
		},
		{
			name: "Floor Copier Color",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsColor:    true,
				IsCopier:   true,
				FormFactor: FormFactorFloorCopier,
			},
			wantType: "Color MFP",
		},
		{
			name: "Desktop Color MFP (no form factor)",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsColor:    true,
				IsCopier:   true,
				FormFactor: "",
			},
			wantType: "Color MFP",
		},
		{
			name: "Desktop Mono Printer (no form factor)",
			caps: DeviceCapabilities{
				IsPrinter:  true,
				IsMono:     true,
				FormFactor: "",
			},
			wantType: "Mono Printer",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyDeviceType(tc.caps)
			if got != tc.wantType {
				t.Errorf("classifyDeviceType() = %q, want %q", got, tc.wantType)
			}
		})
	}
}
