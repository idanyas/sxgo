package sxgo

import "encoding/binary"

// header stores information from the beginning of the SxGeo database file.
// This struct is internal.
type header struct {
	version      uint8  // Database version (usually 22 for v2.2)
	timestamp    uint32 // Database creation timestamp (Unix epoch)
	dbType       uint8  // Database type identifier
	charset      uint8  // Database character set identifier
	byteIndexLen uint8  // Number of entries in the first-byte index
	mainIndexLen uint16 // Number of entries in the main index
	rangeBlocks  uint16 // Number of DB items covered by one main index entry
	dbItems      uint32 // Total number of IP range items in the database
	idLen        uint8  // Length of the location ID (1, 2, 3 or 4 bytes)
	maxRegion    uint16 // Maximum size of a region record
	maxCity      uint16 // Maximum size of a city record
	regionSize   uint32 // Total size of the region data block
	citySize     uint32 // Total size of the city data block
	maxCountry   uint16 // Maximum size of a country record
	countrySize  uint32 // Total size of the country data block (often part of city block in v2.2)
	packSize     uint16 // Size of the packing format strings block
}

// parseHeader reads the header block from the byte slice.
func parseHeader(data []byte) (*header, bool) {
	if len(data) < dbHeaderLen || string(data[0:3]) != dbSig {
		return nil, false
	}

	h := &header{
		version:      data[3],
		timestamp:    binary.BigEndian.Uint32(data[4:8]),
		dbType:       data[8],
		charset:      data[9],
		byteIndexLen: data[10],
		mainIndexLen: binary.BigEndian.Uint16(data[11:13]),
		rangeBlocks:  binary.BigEndian.Uint16(data[13:15]),
		dbItems:      binary.BigEndian.Uint32(data[15:19]),
		idLen:        data[19],
		maxRegion:    binary.BigEndian.Uint16(data[20:22]),
		maxCity:      binary.BigEndian.Uint16(data[22:24]),
		regionSize:   binary.BigEndian.Uint32(data[24:28]),
		citySize:     binary.BigEndian.Uint32(data[28:32]),
		maxCountry:   binary.BigEndian.Uint16(data[32:34]),
		countrySize:  binary.BigEndian.Uint32(data[34:38]),
		packSize:     binary.BigEndian.Uint16(data[38:40]),
	}

	// Basic validation of critical header values
	if h.byteIndexLen == 0 || h.mainIndexLen == 0 || h.rangeBlocks == 0 || h.dbItems == 0 || h.idLen == 0 || h.idLen > 4 {
		return nil, false
	}

	return h, true
}
