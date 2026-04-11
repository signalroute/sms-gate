// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package at implements AT command serialization and SMS PDU encoding/decoding
// per ITU-T V.250 and 3GPP TS 23.038 / 27.007.
package at

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// ── GSM 7-bit basic character set (3GPP TS 23.038 Table 1) ────────────────

// gsm7Charset maps GSM7 code point (0-127) to Unicode rune.
var gsm7Charset = [128]rune{
	'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
	'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', 0x1B, 'Æ', 'æ', 'ß', 'É',
	' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
	'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
}

// gsm7Extension maps GSM7 extension table code points (after ESC 0x1B).
var gsm7Extension = map[byte]rune{
	0x0A: '\f', // form feed
	0x14: '^',
	0x28: '{',
	0x29: '}',
	0x2F: '\\',
	0x3C: '[',
	0x3D: '~',
	0x3E: ']',
	0x40: '|',
	0x65: '€',
}

// runeToGSM7 maps Unicode runes to GSM7 basic charset code points.
var runeToGSM7 map[rune]byte

func init() {
	runeToGSM7 = make(map[rune]byte, 128)
	for i, r := range gsm7Charset {
		if r != 0 {
			runeToGSM7[r] = byte(i)
		}
	}
}

// IsGSM7 reports whether all runes in s are representable in GSM7.
func IsGSM7(s string) bool {
	for _, r := range s {
		if _, ok := runeToGSM7[r]; !ok {
			// Check extension table
			found := false
			for _, er := range gsm7Extension {
				if er == r {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// ── PDU unpacking / packing ────────────────────────────────────────────────

// unpackGSM7 unpacks nChars 7-bit characters from packed data.
func unpackGSM7(data []byte, nChars int) []byte {
	out := make([]byte, nChars)
	for i := 0; i < nChars; i++ {
		bytePos := (i * 7) / 8
		bitPos := uint((i * 7) % 8)
		b := (data[bytePos] >> bitPos) & 0x7F
		if bitPos > 1 && bytePos+1 < len(data) {
			b |= (data[bytePos+1] << (8 - bitPos)) & 0x7F
		}
		out[i] = b
	}
	return out
}

// packGSM7 packs 7-bit characters into bytes.
func packGSM7(chars []byte) []byte {
	n := len(chars)
	packed := make([]byte, (n*7+7)/8)
	for i, c := range chars {
		bytePos := (i * 7) / 8
		bitPos := uint((i * 7) % 8)
		packed[bytePos] |= c << bitPos
		if bitPos > 1 && bytePos+1 < len(packed) {
			packed[bytePos+1] |= c >> (8 - bitPos)
		}
	}
	return packed
}

// decodeGSM7Text converts GSM7 code points to a UTF-8 string.
func decodeGSM7Text(codes []byte) string {
	var sb strings.Builder
	for i := 0; i < len(codes); i++ {
		c := codes[i]
		if c == 0x1B { // escape
			if i+1 < len(codes) {
				i++
				if r, ok := gsm7Extension[codes[i]]; ok {
					sb.WriteRune(r)
				}
			}
			continue
		}
		if int(c) < len(gsm7Charset) {
			sb.WriteRune(gsm7Charset[c])
		}
	}
	return sb.String()
}

// encodeGSM7Text converts a UTF-8 string to GSM7 code points.
func encodeGSM7Text(s string) []byte {
	var out []byte
	for _, r := range s {
		if b, ok := runeToGSM7[r]; ok {
			out = append(out, b)
			continue
		}
		// Check extension table
		for code, er := range gsm7Extension {
			if er == r {
				out = append(out, 0x1B, code)
				break
			}
		}
	}
	return out
}

// decodeUCS2 decodes UCS2 big-endian bytes to UTF-8.
func decodeUCS2(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = binary.BigEndian.Uint16(data[i*2:])
	}
	return string(utf16.Decode(u16))
}

// encodeUCS2 encodes a UTF-8 string to UCS2 big-endian bytes.
func encodeUCS2(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	out := make([]byte, len(u16)*2)
	for i, u := range u16 {
		binary.BigEndian.PutUint16(out[i*2:], u)
	}
	return out
}

// ── Address encoding/decoding ──────────────────────────────────────────────

// swapNibbles swaps the low and high nibbles of a BCD byte.
func swapNibbles(b byte) byte { return (b>>4)&0x0F | (b&0x0F)<<4 }

// decodeBCDAddress decodes a BCD-encoded address from raw bytes.
// numDigits is the number of address digits (nibbles).
// addrType 0x91 = international, 0xD0 = alphanumeric.
func decodeBCDAddress(data []byte, numDigits int, addrType byte) string {
	if addrType == 0xD0 {
		// Alphanumeric sender: encoded as GSM7 in the address octets
		codes := unpackGSM7(data, numDigits)
		return decodeGSM7Text(codes)
	}

	var sb strings.Builder
	if addrType == 0x91 {
		sb.WriteByte('+')
	}
	for i := 0; i < numDigits; i++ {
		byteIdx := i / 2
		if byteIdx >= len(data) {
			break
		}
		var nibble byte
		if i%2 == 0 {
			nibble = data[byteIdx] & 0x0F
		} else {
			nibble = (data[byteIdx] >> 4) & 0x0F
		}
		if nibble == 0x0F {
			break // padding
		}
		sb.WriteByte('0' + nibble)
	}
	return sb.String()
}

// encodeBCDAddress encodes an E.164 phone number to BCD bytes.
// Returns (numDigits, typeOfAddress, data).
func encodeBCDAddress(msisdn string) (int, byte, []byte) {
	var addrType byte = 0x81
	digits := msisdn
	if strings.HasPrefix(digits, "+") {
		addrType = 0x91
		digits = digits[1:]
	}
	numDigits := len(digits)
	// Pad to even length with 0xF
	if numDigits%2 != 0 {
		digits += "F"
	}
	data := make([]byte, len(digits)/2)
	for i := range data {
		lo := digits[i*2] - '0'
		hi := digits[i*2+1]
		if hi == 'F' {
			hi = 0x0F
		} else {
			hi -= '0'
		}
		data[i] = lo | (hi << 4)
	}
	return numDigits, addrType, data
}

// decodeSMSC decodes the SMSC address prefix from a PDU.
// Returns the SMSC string and the number of bytes consumed.
func decodeSMSC(raw []byte) (string, int) {
	if len(raw) == 0 {
		return "", 0
	}
	smscLen := int(raw[0])
	if smscLen == 0 {
		return "", 1
	}
	if 1+smscLen > len(raw) {
		return "", 1 + smscLen
	}
	addrType := raw[1]
	smsc := decodeBCDAddress(raw[2:1+smscLen], (smscLen-1)*2, addrType)
	return smsc, 1 + smscLen
}

// decodeSCTS decodes the 7-byte Service Centre Timestamp (3GPP TS 23.040 §9.2.3.11).
func decodeSCTS(data []byte) time.Time {
	if len(data) < 7 {
		return time.Time{}
	}
	yy := int(swapNibbles(data[0]))
	mm := int(swapNibbles(data[1]))
	dd := int(swapNibbles(data[2]))
	hh := int(swapNibbles(data[3]))
	min := int(swapNibbles(data[4]))
	ss := int(swapNibbles(data[5]))

	// Timezone: bits 0-6 = offset in quarter hours, bit 7 = sign
	tzRaw := swapNibbles(data[6])
	tzQuarters := int(tzRaw & 0x7F)
	tzSign := 1
	if tzRaw&0x80 != 0 {
		tzSign = -1
	}
	tzOffset := tzSign * tzQuarters * 15 * 60 // seconds

	year := 2000 + yy
	loc := time.FixedZone("SMS", tzOffset)
	return time.Date(year, time.Month(mm), dd, hh, min, ss, 0, loc)
}

// dcsEncoding returns "GSM7", "8BIT", or "UCS2" based on the DCS byte.
func dcsEncoding(dcs byte) string {
	class := (dcs >> 4) & 0x0F
	switch {
	case class == 0x00 || class == 0x0F:
		// General group or data coding / message class
		enc := (dcs >> 2) & 0x03
		switch enc {
		case 0x00:
			return "GSM7"
		case 0x01:
			return "8BIT"
		case 0x02:
			return "UCS2"
		}
	case class >= 0x04 && class <= 0x07:
		// MWI group - always 7bit
		return "GSM7"
	case class >= 0x08 && class <= 0x0B:
		// MWI group with UCS2
		return "UCS2"
	case class == 0x0C || class == 0x0D:
		return "GSM7"
	case class == 0x0E:
		if (dcs & 0x04) != 0 {
			return "UCS2"
		}
		return "GSM7"
	}
	return "GSM7"
}

// ── Public API ─────────────────────────────────────────────────────────────

// DecodedSMS represents a fully decoded incoming SMS.
type DecodedSMS struct {
	SMSC      string
	Sender    string
	Body      string
	Timestamp time.Time
	PDUHash   string // sha256:<hex>
}

// DecodePDU decodes a PDU hex string (as returned by AT+CMGR) into a DecodedSMS.
// The raw hex string is hashed with SHA-256 for deduplication.
func DecodePDU(hexStr string) (*DecodedSMS, error) {
	hexStr = strings.TrimSpace(strings.ToUpper(hexStr))
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("invalid PDU hex: %w", err)
	}

	sum := sha256.Sum256([]byte(hexStr))
	pduHash := "sha256:" + hex.EncodeToString(sum[:])

	pos := 0

	// SMSC
	smsc, smscConsumed := decodeSMSC(raw)
	pos += smscConsumed

	if pos >= len(raw) {
		return nil, fmt.Errorf("PDU truncated after SMSC")
	}

	// PDU type
	pduType := raw[pos]
	pos++
	mti := pduType & 0x03
	if mti != 0x00 {
		return nil, fmt.Errorf("not SMS-DELIVER (MTI=%d)", mti)
	}

	// Originating Address
	if pos+2 > len(raw) {
		return nil, fmt.Errorf("PDU truncated at OA")
	}
	oaNumDigits := int(raw[pos])
	pos++
	oaType := raw[pos]
	pos++
	oaBytes := (oaNumDigits + 1) / 2
	if pos+oaBytes > len(raw) {
		return nil, fmt.Errorf("PDU truncated at OA digits")
	}
	sender := decodeBCDAddress(raw[pos:pos+oaBytes], oaNumDigits, oaType)
	pos += oaBytes

	// PID
	pos++ // skip

	// DCS
	if pos >= len(raw) {
		return nil, fmt.Errorf("PDU truncated at DCS")
	}
	dcs := raw[pos]
	pos++

	// SCTS (7 bytes)
	if pos+7 > len(raw) {
		return nil, fmt.Errorf("PDU truncated at SCTS")
	}
	ts := decodeSCTS(raw[pos : pos+7])
	pos += 7

	// UDL
	if pos >= len(raw) {
		return nil, fmt.Errorf("PDU truncated at UDL")
	}
	udl := int(raw[pos])
	pos++

	// UD
	enc := dcsEncoding(dcs)
	var body string
	ud := raw[pos:]
	switch enc {
	case "GSM7":
		codes := unpackGSM7(ud, udl)
		body = decodeGSM7Text(codes)
	case "UCS2":
		ucs2Bytes := udl * 2 // UDL in UCS2 is number of characters (2 bytes each)
		if ucs2Bytes > len(ud) {
			ucs2Bytes = len(ud)
		}
		body = decodeUCS2(ud[:ucs2Bytes])
	case "8BIT":
		if udl > len(ud) {
			udl = len(ud)
		}
		body = string(ud[:udl])
	}

	return &DecodedSMS{
		SMSC:      smsc,
		Sender:    sender,
		Body:      body,
		Timestamp: ts,
		PDUHash:   pduHash,
	}, nil
}

// EncodedPDU is the result of encoding an outgoing SMS.
type EncodedPDU struct {
	HexStr   string // PDU hex, without leading 00 (SMSC)
	Length   int    // length for AT+CMGS=<length> (excludes SMSC octet)
	NumParts int    // number of PDU parts (1 for single-part)
}

const (
	maxGSM7Chars = 160
	maxUCS2Chars = 70
	// Concatenated SMS limits (with UDH)
	maxGSM7ConcatChars = 153
	maxUCS2ConcatChars = 67
)

// EncodePDU encodes an outgoing SMS for submission via AT+CMGS.
// encoding must be "GSM7" or "UCS2". If empty, it is auto-detected.
// Returns one EncodedPDU per part (multi-part SMS are split automatically).
func EncodePDU(to, body, encoding string) ([]EncodedPDU, error) {
	if encoding == "" {
		if IsGSM7(body) {
			encoding = "GSM7"
		} else {
			encoding = "UCS2"
		}
	}

	switch encoding {
	case "GSM7":
		return encodeGSM7PDUs(to, body)
	case "UCS2":
		return encodeUCS2PDUs(to, body)
	default:
		return nil, fmt.Errorf("unsupported encoding %q", encoding)
	}
}

func encodeGSM7PDUs(to, body string) ([]EncodedPDU, error) {
	chars := encodeGSM7Text(body)
	nChars := len(chars)

	if nChars <= maxGSM7Chars {
		return []EncodedPDU{buildGSM7PDU(to, chars, 0, 0, 0)}, nil
	}

	// Multi-part
	ref := byte(0x42) // arbitrary reference byte
	var parts []EncodedPDU
	nParts := (nChars + maxGSM7ConcatChars - 1) / maxGSM7ConcatChars
	for i := 0; i < nParts; i++ {
		start := i * maxGSM7ConcatChars
		end := start + maxGSM7ConcatChars
		if end > nChars {
			end = nChars
		}
		parts = append(parts, buildGSM7PDU(to, chars[start:end], ref, byte(nParts), byte(i+1)))
	}
	return parts, nil
}

func encodeUCS2PDUs(to, body string) ([]EncodedPDU, error) {
	runes := []rune(body)
	nChars := len(runes)

	if nChars <= maxUCS2Chars {
		return []EncodedPDU{buildUCS2PDU(to, runes, 0, 0, 0)}, nil
	}

	ref := byte(0x42)
	var parts []EncodedPDU
	nParts := (nChars + maxUCS2ConcatChars - 1) / maxUCS2ConcatChars
	for i := 0; i < nParts; i++ {
		start := i * maxUCS2ConcatChars
		end := start + maxUCS2ConcatChars
		if end > nChars {
			end = nChars
		}
		parts = append(parts, buildUCS2PDU(to, runes[start:end], ref, byte(nParts), byte(i+1)))
	}
	return parts, nil
}

// buildGSM7PDU constructs a single SMS-SUBMIT PDU with GSM7 encoding.
// ref, total, partNum are non-zero for concatenated SMS.
func buildGSM7PDU(to string, chars []byte, ref, total, partNum byte) EncodedPDU {
	multiPart := total > 0

	var pdu []byte

	// SMSC: 0x00 (use modem default)
	pdu = append(pdu, 0x00)

	// PDU type: SMS-SUBMIT
	// Bits: RP=0 UDHI=? SRR=0 VPF=10 (relative) RD=0 MTI=01
	var pduType byte = 0x11 // MTI=01, VPF=10 (relative VP)
	if multiPart {
		pduType |= 0x40 // UDHI = 1
	}
	pdu = append(pdu, pduType)

	// MR: 0x00
	pdu = append(pdu, 0x00)

	// DA
	numDigits, daType, daData := encodeBCDAddress(to)
	pdu = append(pdu, byte(numDigits))
	pdu = append(pdu, daType)
	pdu = append(pdu, daData...)

	// PID: 0x00
	pdu = append(pdu, 0x00)

	// DCS: GSM7 = 0x00
	pdu = append(pdu, 0x00)

	// VP: 0xAA = 4 days (relative)
	pdu = append(pdu, 0xAA)

	// Build UD
	var udh []byte
	if multiPart {
		// UDH: 05 00 03 <ref> <total> <partNum>
		udh = []byte{0x05, 0x00, 0x03, ref, total, partNum}
	}

	packed := packGSM7(chars)

	// UDL: number of septets including UDH padding
	udl := len(chars)
	if multiPart {
		// UDH is 6 bytes = 6*8/7 = 6.857 septets → 7 padding septets
		udl += 7
	}
	pdu = append(pdu, byte(udl))

	if multiPart {
		// Prepend UDH; the packed data for the body must be padded by
		// the number of fill bits so the body starts on a septet boundary.
		// UDH occupies 7 septets (6 bytes), so fill = 7 * 7 - 6*8 = 49-48 = 1 bit
		// Pack UDH + body together with 1 fill bit shift.
		combined := append(udh, repackWithFill(chars, 1)...)
		pdu = append(pdu, combined...)
	} else {
		pdu = append(pdu, packed...)
	}

	hexStr := strings.ToUpper(hex.EncodeToString(pdu))
	// Length for AT+CMGS excludes the first byte (SMSC 0x00)
	length := len(pdu) - 1

	return EncodedPDU{HexStr: hexStr, Length: length, NumParts: int(total)}
}

// repackWithFill packs chars with a leading fill of fillBits bits.
func repackWithFill(chars []byte, fillBits int) []byte {
	// Shift each septet right by fillBits to accommodate the UDH.
	n := len(chars)
	totalSeptets := n + fillBits // conceptually
	_ = totalSeptets
	// Simple approach: convert to bit stream, insert fill, repack.
	bits := make([]int, n*7+fillBits)
	for i := 0; i < fillBits; i++ {
		bits[i] = 0 // fill bits are zero
	}
	for i, c := range chars {
		base := fillBits + i*7
		for b := 0; b < 7; b++ {
			bits[base+b] = int((c >> uint(b)) & 1)
		}
	}
	out := make([]byte, (len(bits)+7)/8)
	for i, bit := range bits {
		if bit != 0 {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return out
}

func buildUCS2PDU(to string, runes []rune, ref, total, partNum byte) EncodedPDU {
	multiPart := total > 0

	var pdu []byte
	pdu = append(pdu, 0x00) // SMSC

	var pduType byte = 0x11
	if multiPart {
		pduType |= 0x40
	}
	pdu = append(pdu, pduType)
	pdu = append(pdu, 0x00) // MR

	numDigits, daType, daData := encodeBCDAddress(to)
	pdu = append(pdu, byte(numDigits))
	pdu = append(pdu, daType)
	pdu = append(pdu, daData...)

	pdu = append(pdu, 0x00) // PID
	pdu = append(pdu, 0x08) // DCS: UCS2
	pdu = append(pdu, 0xAA) // VP

	body := encodeUCS2(string(runes))

	var udh []byte
	if multiPart {
		udh = []byte{0x05, 0x00, 0x03, ref, total, partNum}
	}

	udl := len(body) + len(udh)
	pdu = append(pdu, byte(udl))
	pdu = append(pdu, udh...)
	pdu = append(pdu, body...)

	hexStr := strings.ToUpper(hex.EncodeToString(pdu))
	return EncodedPDU{HexStr: hexStr, Length: len(pdu) - 1, NumParts: int(total)}
}
