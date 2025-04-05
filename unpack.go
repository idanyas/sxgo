package sxgo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// unpack decodes a byte slice (`data`) into a map based on the Sypex Geo pack format string (`format`).
// Format examples: "Slat/Slon", "Cid/c6iso/Slat/Slon/Nregion_seek/Tcountry_id/..."
// Assumes LittleEndian for multi-byte fields within packed data based on observed PHP behavior.
// Internal function.
func unpack(format string, data []byte) (map[string]interface{}, error) {
	if len(data) == 0 {
		return make(map[string]interface{}), nil // Nothing to unpack
	}
	if format == "" {
		return nil, errors.New("unpack format string is empty")
	}

	result := make(map[string]interface{})
	parts := strings.Split(format, "/")
	offset := 0
	dataLen := len(data)

	for _, part := range parts {
		if offset >= dataLen {
			// We've consumed all data, but there are more format parts.
			// This might be okay if remaining parts are optional or the data was truncated.
			// Return the partially filled map.
			// fmt.Printf("Warning: unpack consumed all data (%d bytes) but format string '%s' has remaining parts starting with '%s'\n", dataLen, format, part)
			break
		}

		spec := strings.SplitN(part, ":", 2)
		if len(spec) != 2 {
			return result, fmt.Errorf("invalid unpack format part: %q in format %q", part, format)
		}
		typeFormat, name := spec[0], spec[1]

		var value interface{}
		var length int
		var err error

		typeCode := typeFormat[0]
		typeLenStr := ""
		if len(typeFormat) > 1 {
			typeLenStr = typeFormat[1:]
		}

		// --- Determine length and read value based on type ---
		switch typeCode {
		case 't': // signed char (int8)
			length = 1
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			value = int8(data[offset])
		case 'T': // unsigned char (uint8)
			length = 1
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			value = data[offset]
		case 's': // signed short (int16, Little Endian)
			length = 2
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			value = int16(binary.LittleEndian.Uint16(data[offset : offset+length]))
		case 'S': // unsigned short (uint16, Little Endian)
			length = 2
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			value = binary.LittleEndian.Uint16(data[offset : offset+length])
		case 'm': // signed medium int (int32, 3 bytes, Little Endian)
			length = 3
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			b := data[offset : offset+length]
			if b[2]&0x80 != 0 { // Check sign bit in the most significant byte (LE order)
				value = int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | 0xFF000000) // Sign extend
			} else {
				value = int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16)
			}
		case 'M': // unsigned medium int (uint32, 3 bytes, Little Endian)
			length = 3
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			b := data[offset : offset+length]
			value = uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
		case 'i': // signed int (int32, Little Endian)
			length = 4
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			value = int32(binary.LittleEndian.Uint32(data[offset : offset+length]))
		case 'I': // unsigned int (uint32, Little Endian)
			length = 4
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			value = binary.LittleEndian.Uint32(data[offset : offset+length])
		case 'f': // float (float32, Little Endian)
			length = 4
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			bits := binary.LittleEndian.Uint32(data[offset : offset+length])
			value = float64(math.Float32frombits(bits)) // Store as float64 for consistency
		case 'd': // double (float64, Little Endian)
			length = 8
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			bits := binary.LittleEndian.Uint64(data[offset : offset+length])
			value = math.Float64frombits(bits)
		case 'n': // packed decimal (int16 as float / 10^scale, LE)
			length = 2
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			num := int16(binary.LittleEndian.Uint16(data[offset : offset+length]))
			scale, _ := strconv.Atoi(typeLenStr) // Default scale 0 if empty/invalid
			value = float64(num) / math.Pow10(scale)
		case 'N': // packed decimal (int32 as float / 10^scale, LE)
			length = 4
			if offset+length > dataLen {
				err = io.ErrUnexpectedEOF
				break
			}
			num := int32(binary.LittleEndian.Uint32(data[offset : offset+length]))
			scale, _ := strconv.Atoi(typeLenStr) // Default scale 0 if empty/invalid
			value = float64(num) / math.Pow10(scale)
		case 'c': // fixed length string (null-padded?)
			var cerr error
			length, cerr = strconv.Atoi(typeLenStr)
			if cerr != nil || length <= 0 {
				err = fmt.Errorf("invalid length '%s' for c format", typeLenStr)
				break
			}
			if offset+length > dataLen {
				// Allow partial read if data truncated?
				// PHP might read partially. Let's read what's available.
				length = dataLen - offset // Adjust length to remaining data
				// err = io.ErrUnexpectedEOF // Keep track that we hit the end
			}
			// Trim trailing null bytes and potentially spaces based on observed data
			value = strings.TrimRight(string(data[offset:offset+length]), "\x00 ")
		case 'b': // null-terminated string
			end := offset
			for end < dataLen && data[end] != 0 {
				end++
			}
			if end >= dataLen {
				// No null terminator found within available data. Read rest as string.
				value = string(data[offset:])
				length = dataLen - offset
				// err = errors.New("null terminator not found for 'b' type") // Informative error?
			} else {
				// Null terminator found at 'end'
				value = string(data[offset:end])
				length = (end - offset) + 1 // Consume the null terminator as well
			}
		default:
			err = fmt.Errorf("unsupported format specifier: %q", typeCode)
		} // end switch

		// --- Handle errors and advance offset ---
		if err != nil {
			// Provide context about the field being processed
			errContext := fmt.Errorf("field %q (format %q): %w", name, typeFormat, err)
			// If EOF, maybe return partial result? For now, return error.
			if errors.Is(err, io.ErrUnexpectedEOF) {
				errContext = fmt.Errorf("field %q (format %q): unexpected end of data (offset %d, need %d, total %d)", name, typeFormat, offset, length, dataLen)
			}
			return result, errContext // Return partially unpacked data and the error
		}

		result[name] = value
		offset += length

	} // end for loop over parts

	return result, nil // Return fully unpacked data
}

// --- Helper Getters for Unpacked Map ---
// These provide type safety and default values when accessing the map.

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getUint8(m map[string]interface{}, key string) uint8 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case uint8:
			return v
		case int8:
			return uint8(v)
		case int:
			if v >= 0 && v <= math.MaxUint8 {
				return uint8(v)
			}
		case uint16:
			if v <= math.MaxUint8 {
				return uint8(v)
			}
		case uint32:
			if v <= math.MaxUint8 {
				return uint8(v)
			}
		case float64: // Handle potential float representation
			if v >= 0 && v <= math.MaxUint8 && v == math.Floor(v) {
				return uint8(v)
			}
		case float32:
			f64 := float64(v)
			if f64 >= 0 && f64 <= math.MaxUint8 && f64 == math.Floor(f64) {
				return uint8(f64)
			}
		}
	}
	return 0
}

func getUint32(m map[string]interface{}, key string) uint32 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case uint32:
			return v
		case int32:
			if v >= 0 {
				return uint32(v)
			}
		case int:
			if v >= 0 && v <= math.MaxUint32 {
				return uint32(v)
			}
		case uint16:
			return uint32(v)
		case uint8:
			return uint32(v)
		case float64: // Handle potential float representation
			if v >= 0 && v <= math.MaxUint32 && v == math.Floor(v) {
				return uint32(v)
			}
		case float32:
			f64 := float64(v)
			if f64 >= 0 && f64 <= math.MaxUint32 && f64 == math.Floor(f64) {
				return uint32(f64)
			}
		}
	}
	return 0
}

func getFloat(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		// Allow conversion from integer types if needed
		case int:
			return float64(v)
		case int8:
			return float64(v)
		case int16:
			return float64(v)
		case int32:
			return float64(v)
		case uint:
			return float64(v)
		case uint8:
			return float64(v)
		case uint16:
			return float64(v)
		case uint32:
			return float64(v)
		}
	}
	return 0.0
}
