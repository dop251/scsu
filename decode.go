package scsu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

type Decoder struct {
	scsu
	brd       io.ByteReader
	bytesRead int

	unicodeMode bool
}

var (
	ErrIllegalInput = errors.New("illegal input")
)

func NewDecoder(r io.ByteReader) *Decoder {
	d := &Decoder{
		brd: r,
	}
	d.init()
	return d
}

func (d *Decoder) readByte() (byte, error) {
	b, err := d.brd.ReadByte()
	if err == nil {
		d.bytesRead++
	}
	return b, err
}

/** (re-)define (and select) a dynamic window
  A sliding window position cannot start at any Unicode value,
  so rather than providing an absolute offset, this function takes
  an index value which selects among the possible starting values.

  Most scripts in Unicode start on or near a half-block boundary
  so the default behaviour is to multiply the index by 0x80. Han,
  Hangul, Surrogates and other scripts between 0x3400 and 0xDFFF
  show very poor locality--therefore no sliding window can be set
  there. A jumpOffset is added to the index value to skip that region,
  and only 167 index values total are required to select all eligible
  half-blocks.

  Finally, a few scripts straddle half block boundaries. For them, a
  table of fixed offsets is used, and the index values from 0xF9 to
  0xFF are used to select these special offsets.

  After (re-)defining a windows location it is selected so it is ready
  for use.

  Recall that all Windows are of the same length (128 code positions).
*/
func (d *Decoder) defineWindow(iWindow int, offset byte) error {
	// 0 is a reserved value
	if offset == 0 {
		return ErrIllegalInput
	}
	if offset < gapThreshold {
		d.dynamicOffset[iWindow] = int32(offset) << 7
	} else if offset < reservedStart {
		d.dynamicOffset[iWindow] = (int32(offset) << 7) + gapOffset
	} else if offset < fixedThreshold {
		return fmt.Errorf("offset = %d", offset)
	} else {
		d.dynamicOffset[iWindow] = fixedOffset[offset-fixedThreshold]
	}

	// make the redefined window the active one
	d.window = iWindow
	return nil
}

/** (re-)define (and select) a window as an extended dynamic window
  The surrogate area in Unicode allows access to 2**20 codes beyond the
  first 64K codes by combining one of 1024 characters from the High
  Surrogate Area with one of 1024 characters from the Low Surrogate
  Area (see Unicode 2.0 for the details).

  The tags SDX and UDX set the window such that each subsequent byte in
  the range 80 to FF represents a surrogate pair. The following diagram
  shows how the bits in the two bytes following the SDX or UDX, and a
  subsequent data byte, map onto the bits in the resulting surrogate pair.

   hbyte         lbyte          data
  nnnwwwww      zzzzzyyy      1xxxxxxx

   high-surrogate     low-surrogate
  110110wwwwwzzzzz   110111yyyxxxxxxx

  @param chOffset - Since the three top bits of chOffset are not needed to
  set the location of the extended Window, they are used instead
  to select the window, thereby reducing the number of needed command codes.
  The bottom 13 bits of chOffset are used to calculate the offset relative to
  a 7 bit input data byte to yield the 20 bits expressed by each surrogate pair.
  **/
func (d *Decoder) defineExtendedWindow(chOffset uint16) {
	// The top 3 bits of iOffsetHi are the window index
	window := chOffset >> 13

	// Calculate the new offset
	d.dynamicOffset[window] = ((int32(chOffset) & 0x1FFF) << 7) + (1 << 16)

	// make the redefined window the active one
	d.window = int(window)
}

// convert an io.EOF into io.ErrUnexpectedEOF
func unexpectedEOF(e error) error {
	if errors.Is(e, io.EOF) {
		return io.ErrUnexpectedEOF
	}

	return e
}

func (d *Decoder) expandUnicode() (rune, error) {
	for {
		b, err := d.readByte()
		if err != nil {
			return 0, err
		}
		if b >= UC0 && b <= UC7 {
			d.window = int(b) - UC0
			d.unicodeMode = false
			return -1, nil
		}
		if b >= UD0 && b <= UD7 {
			b1, err := d.readByte()
			if err != nil {
				return 0, unexpectedEOF(err)
			}
			d.unicodeMode = false
			return -1, d.defineWindow(int(b)-UD0, b1)
		}
		if b == UDX {
			c, err := d.readUint16()
			if err != nil {
				return 0, unexpectedEOF(err)
			}
			d.defineExtendedWindow(c)
			d.unicodeMode = false
			return -1, nil
		}
		if b == UQU {
			r, err := d.readUint16()
			if err != nil {
				return 0, err
			}
			return rune(r), nil
		} else {
			b1, err := d.readByte()
			if err != nil {
				return 0, unexpectedEOF(err)
			}
			ch := rune(uint16FromTwoBytes(b, b1))
			if utf16.IsSurrogate(ch) {
				ch1, err := d.readUint16()
				if err != nil {
					return 0, unexpectedEOF(err)
				}
				surrLo := rune(ch1)
				if !utf16.IsSurrogate(surrLo) {
					return 0, ErrIllegalInput
				}
				return utf16.DecodeRune(ch, surrLo), nil
			}
			return ch, nil
		}
	}
}

func (d *Decoder) readUint16() (uint16, error) {
	b1, err := d.readByte()
	if err != nil {
		return 0, unexpectedEOF(err)
	}
	b2, err := d.readByte()
	if err != nil {
		return 0, unexpectedEOF(err)
	}
	return uint16FromTwoBytes(b1, b2), nil
}

func uint16FromTwoBytes(hi, lo byte) uint16 {
	return uint16(hi)<<8 | uint16(lo)
}

/** expand portion of the input that is in single byte mode **/
func (d *Decoder) expandSingleByte() (rune, error) {
	for {
		b, err := d.readByte()
		if err != nil {
			return 0, err
		}
		staticWindow := 0
		dynamicWindow := d.window

		switch b {
		case SQ0, SQ1, SQ2, SQ3, SQ4, SQ5, SQ6, SQ7:
			// Select window pair to quote from
			dynamicWindow = int(b) - SQ0
			staticWindow = dynamicWindow
			b, err = d.readByte()
			if err != nil {
				return 0, unexpectedEOF(err)
			}
			fallthrough
		default:
			// output as character
			if b < 0x80 {
				// use static window
				return int32(b) + staticOffset[staticWindow], nil
			} else {
				ch := int32(b) - 0x80
				ch += d.dynamicOffset[dynamicWindow]
				return ch, nil
			}
		case SDX:
			// define a dynamic window as extended
			ch, err := d.readUint16()
			if err != nil {
				return 0, unexpectedEOF(err)
			}
			d.defineExtendedWindow(ch)
		case SD0, SD1, SD2, SD3, SD4, SD5, SD6, SD7:
			// Position a dynamic Window
			b1, err := d.readByte()
			if err != nil {
				return 0, unexpectedEOF(err)
			}
			err = d.defineWindow(int(b)-SD0, b1)
			if err != nil {
				return 0, err
			}
		case SC0, SC1, SC2, SC3, SC4, SC5, SC6, SC7:
			// Select a new dynamic Window
			d.window = int(b) - SC0
		case SCU:
			// switch to Unicode mode and continue parsing
			d.unicodeMode = true
			return -1, nil
		case SQU:
			// directly extract one Unicode character
			ch, err := d.readUint16()
			if err != nil {
				return 0, err
			}
			return rune(ch), nil
		case Srs:
			return 0, ErrIllegalInput
		}
	}
}

func (d *Decoder) readRune() (rune, error) {
	for {
		var r rune
		var err error
		if d.unicodeMode {
			r, err = d.expandUnicode()
		} else {
			r, err = d.expandSingleByte()
		}
		if err != nil {
			return 0, err
		}
		if r == -1 {
			continue
		}
		return r, nil
	}
}

// ReadRune reads a single SCSU encoded Unicode character
// and returns the rune and the amount of bytes consumed. If no character is
// available, err will be set.
func (d *Decoder) ReadRune() (rune, int, error) {
	pr := d.bytesRead
	r, err := d.readRune()
	return r, d.bytesRead - pr, err
}

// ReadString reads all available input as a string.
// It keeps reading the source reader until it returns io.EOF or an error occurs.
// In case of io.EOF the error returned by ReadString will be nil.
func (d *Decoder) ReadString() (string, error) {
	var sb strings.Builder
	for {
		r, err := d.readRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
		sb.WriteRune(r)
	}
	return sb.String(), nil
}

// Decode a byte array as a string.
func Decode(b []byte) (string, error) {
	return NewDecoder(bytes.NewBuffer(b)).ReadString()
}
