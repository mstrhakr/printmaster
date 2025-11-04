package util

import (
	"testing"
)

func TestDecodeOctetString_UTF8(t *testing.T) {
	b := []byte("Black Cartridge HP CE278A")
	got := DecodeOctetString(b)
	want := "Black Cartridge HP CE278A"
	if got != want {
		t.Fatalf("DecodeOctetString UTF8: got %q want %q", got, want)
	}
}

func TestDecodeOctetString_NonUTF8(t *testing.T) {
	// invalid UTF-8 single byte > 127 should be mapped to same codepoint
	b := []byte{0xff, 0x20, 0x41}
	got := DecodeOctetString(b)
	// 0xff will map to rune 255; string will contain that rune plus space and A
	if got == "" {
		t.Fatalf("expected non-empty string for non-UTF8 input")
	}
}

func TestCoerceToInt_HexString(t *testing.T) {
	v := "0xb3e7"
	got, ok := CoerceToInt(v)
	if !ok {
		t.Fatalf("CoerceToInt failed to parse %q", v)
	}
	// 0xb3e7 == 46055
	if got != 46055 {
		t.Fatalf("CoerceToInt hex: got %d want %d", got, 46055)
	}
}

func TestCoerceToInt_BytesDecimal(t *testing.T) {
	v := []byte("12345")
	got, ok := CoerceToInt(v)
	if !ok || got != 12345 {
		t.Fatalf("CoerceToInt bytes decimal: got %d ok=%v", got, ok)
	}
}
