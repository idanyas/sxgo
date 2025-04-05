package sxgo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// SxGeo provides methods for querying a Sypex Geo database file.
type SxGeo struct {
	f            *os.File // File handle (nil in ModeMemory after init)
	header       *header  // Parsed database header
	packFormats  []string // Unpacking formats for country, region, city
	dbBegin      int64    // Offset where the main DB blocks start
	regionsBegin int64    // Offset where region data starts
	citiesBegin  int64    // Offset where city data starts
	blockSize    uint32   // Size of one IP range block in the main DB (3 bytes IP + ID bytes)

	// Mode flags
	memoryMode bool
	batchMode  bool

	// Data and indexes (populated based on mode)
	byteIndexStr []byte   // Raw byte index (used in ModeFile)
	mainIndexStr []byte   // Raw main index (used in ModeFile)
	byteIndexArr []uint32 // Parsed byte index (used if batchMode or memoryMode)
	mainIndexArr []uint32 // Parsed main index (used if batchMode or memoryMode)
	dbData       []byte   // Main database blocks (used in ModeMemory)
	regionsData  []byte   // Region data (used in ModeMemory)
	citiesData   []byte   // City data (used in ModeMemory)
}

// New creates a new SxGeo instance to query the database file.
//
// dbFile is the path to the Sypex Geo .dat file (v2.2 format expected).
// mode determines how the database is accessed (ModeFile, ModeMemory, ModeBatch).
// Use ModeMemory for best performance if memory usage is acceptable.
// Combine ModeBatch with ModeMemory (ModeMemory | ModeBatch) for potentially
// faster lookups in high-throughput scenarios by pre-parsing indexes.
func New(dbFile string, mode uint) (*SxGeo, error) {
	f, err := os.Open(dbFile)
	if err != nil {
		return nil, fmt.Errorf("sxgo: failed to open db file %q: %w", dbFile, err)
	}

	s := &SxGeo{
		f:          f,
		memoryMode: (mode & ModeMemory) != 0,
		batchMode:  (mode & ModeBatch) != 0,
	}

	// Read and parse header
	headerBytes := make([]byte, dbHeaderLen)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		f.Close()
		return nil, fmt.Errorf("sxgo: failed to read header from %q: %w", dbFile, err)
	}

	h, ok := parseHeader(headerBytes)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("sxgo: invalid header or signature in %q", dbFile)
	}
	s.header = h
	s.blockSize = dbBlockLenOffset + uint32(s.header.idLen)

	// Read pack formats if they exist
	if s.header.packSize > 0 {
		packBytes := make([]byte, s.header.packSize)
		if _, err := io.ReadFull(f, packBytes); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: failed to read pack formats from %q: %w", dbFile, err)
		}
		// Split and remove potential empty string at the end if format ends with \x00
		s.packFormats = strings.Split(strings.TrimRight(string(packBytes), "\x00"), "\x00")
	} else {
		// Need at least city/country formats for city DBs
		if s.header.maxCity > 0 {
			f.Close()
			return nil, fmt.Errorf("sxgo: database %q is a City DB but lacks pack formats", dbFile)
		}
		// Allow country DB without pack formats (though country names won't be available)
		s.packFormats = []string{} // Ensure it's initialized
	}

	// --- Read Indexes ---
	byteIndexSize := int64(s.header.byteIndexLen) * 4
	mainIndexSize := int64(s.header.mainIndexLen) * 4
	useParsedIndexes := s.batchMode || s.memoryMode

	if useParsedIndexes {
		// Read raw indexes first
		rawBIdx := make([]byte, byteIndexSize)
		rawMIdx := make([]byte, mainIndexSize)
		if _, err := io.ReadFull(f, rawBIdx); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: failed to read byte index from %q: %w", dbFile, err)
		}
		if _, err := io.ReadFull(f, rawMIdx); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: failed to read main index from %q: %w", dbFile, err)
		}

		// Parse into arrays
		s.byteIndexArr = make([]uint32, s.header.byteIndexLen)
		s.mainIndexArr = make([]uint32, s.header.mainIndexLen)
		for i := 0; i < int(s.header.byteIndexLen); i++ {
			s.byteIndexArr[i] = binary.BigEndian.Uint32(rawBIdx[i*4 : (i+1)*4])
		}
		for i := 0; i < int(s.header.mainIndexLen); i++ {
			s.mainIndexArr[i] = binary.BigEndian.Uint32(rawMIdx[i*4 : (i+1)*4])
		}

		// Keep raw slices only if needed for pure file mode (not typical here)
		// Or potentially for a hybrid mode not currently implemented.
		// For simplicity, we don't store them if using parsed indexes.
		// If ModeFile is somehow combined, store them:
		// if !s.memoryMode && !s.batchMode { s.byteIndexStr = rawBIdx; s.mainIndexStr = rawMIdx }

	} else { // File mode - read raw bytes directly
		s.byteIndexStr = make([]byte, byteIndexSize)
		s.mainIndexStr = make([]byte, mainIndexSize)
		if _, err := io.ReadFull(f, s.byteIndexStr); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: failed to read byte index from %q: %w", dbFile, err)
		}
		if _, err := io.ReadFull(f, s.mainIndexStr); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: failed to read main index from %q: %w", dbFile, err)
		}
	}

	// Store current position as db_begin and calculate data block offsets
	s.dbBegin, err = f.Seek(0, io.SeekCurrent)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("sxgo: failed get db_begin offset in %q: %w", dbFile, err)
	}
	s.regionsBegin = s.dbBegin + int64(s.header.dbItems*s.blockSize)
	s.citiesBegin = s.regionsBegin + int64(s.header.regionSize)

	// --- Load Data into Memory if Requested ---
	if s.memoryMode {
		// Load Main DB Data
		dbSize := int64(s.header.dbItems * s.blockSize)
		s.dbData = make([]byte, dbSize)
		// Seek back to start of DB data before reading
		if _, err := f.Seek(s.dbBegin, io.SeekStart); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: memory mode failed to seek to db data start in %q: %w", dbFile, err)
		}
		if _, err := io.ReadFull(f, s.dbData); err != nil {
			f.Close()
			return nil, fmt.Errorf("sxgo: failed to read db data into memory from %q: %w", dbFile, err)
		}

		// Load Regions Data (if exists)
		if s.header.regionSize > 0 {
			s.regionsData = make([]byte, s.header.regionSize)
			if _, err := f.Seek(s.regionsBegin, io.SeekStart); err != nil {
				f.Close()
				return nil, fmt.Errorf("sxgo: memory mode failed to seek to regions data start in %q: %w", dbFile, err)
			}
			if _, err := io.ReadFull(f, s.regionsData); err != nil {
				f.Close()
				return nil, fmt.Errorf("sxgo: failed to read regions data into memory from %q: %w", dbFile, err)
			}
		}

		// Load Cities Data (if exists - includes country data in v2.2)
		if s.header.citySize > 0 {
			s.citiesData = make([]byte, s.header.citySize)
			if _, err := f.Seek(s.citiesBegin, io.SeekStart); err != nil {
				f.Close()
				return nil, fmt.Errorf("sxgo: memory mode failed to seek to cities data start in %q: %w", dbFile, err)
			}
			if _, err := io.ReadFull(f, s.citiesData); err != nil {
				f.Close()
				return nil, fmt.Errorf("sxgo: failed to read cities data into memory from %q: %w", dbFile, err)
			}
		}

		// Close the file after loading into memory
		err = f.Close()
		s.f = nil // Set file handle to nil
		if err != nil {
			// Non-fatal error, as data is in memory, but good to know.
			// Could log this? For now, just ignore potential close error.
			// return nil, fmt.Errorf("sxgo: error closing file after memory load %q: %w", dbFile, err)
		}
	}

	return s, nil
}

// Close releases resources used by SxGeo.
// It's primarily important to call this if using ModeFile to close the file handle.
// It's safe to call even if using ModeMemory (it becomes a no-op).
func (s *SxGeo) Close() error {
	if s.f != nil {
		err := s.f.Close()
		s.f = nil // Ensure it's nil after closing
		if err != nil {
			return fmt.Errorf("sxgo: error closing database file: %w", err)
		}
	}
	// Clear memory-loaded data? Optional, GC will handle it eventually.
	// s.dbData = nil
	// s.regionsData = nil
	// s.citiesData = nil
	// s.byteIndexArr = nil
	// s.mainIndexArr = nil
	return nil
}

// ip2long converts an IPv4 address string to its big-endian uint32 representation.
// Returns 0 and false if the IP is invalid or not IPv4.
// This function is internal.
func ip2long(ipStr string) (uint32, bool) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, false
	}
	ipv4 := ip.To4() // Converts IPv4-mapped IPv6 addresses too
	if ipv4 == nil {
		return 0, false // Not an IPv4 address
	}
	return binary.BigEndian.Uint32(ipv4), true
}

// decodeID converts ID bytes (big-endian) to uint32 based on header.idLen.
// This function is internal.
func (s *SxGeo) decodeID(idBytes []byte) (uint32, error) {
	expectedLen := int(s.header.idLen)
	if len(idBytes) != expectedLen {
		return 0, fmt.Errorf("incorrect number of bytes for ID: expected %d, got %d", expectedLen, len(idBytes))
	}
	switch expectedLen {
	case 1:
		return uint32(idBytes[0]), nil
	case 2:
		return uint32(binary.BigEndian.Uint16(idBytes)), nil
	case 3: // Special case for 3 bytes (Big Endian)
		return uint32(idBytes[0])<<16 | uint32(idBytes[1])<<8 | uint32(idBytes[2]), nil
	case 4:
		return binary.BigEndian.Uint32(idBytes), nil
	default:
		// Should be caught by header validation in New()
		return 0, fmt.Errorf("unsupported ID length in header: %d", expectedLen)
	}
}

// Get retrieves location information based on the database type.
// For City databases (SxGeoCity*.dat), it returns city, region (optional), and country info.
// For Country databases (SxGeoCountry.dat), it returns only the country ISO code.
// Returns (nil, nil) if the IP is not found or belongs to a reserved range.
// Returns (nil, error) for database access errors or invalid IP format.
// Note: The return type is interface{} for compatibility with both DB types.
// Consider using more specific methods like GetCityFull or GetCountry if you know the DB type.
func (s *SxGeo) Get(ip string) (interface{}, error) {
	if s.header.maxCity > 0 { // City database
		// Delegates to GetCityFull for consistency, as GetCity might omit region info
		// needed for a complete picture compared to just country ISO.
		// If performance is critical and only basic city/country needed, could call GetCity.
		return s.GetCityFull(ip)
	}
	// Country database
	return s.GetCountry(ip)
}

// GetCountry retrieves the two-letter ISO 3166-1 alpha-2 country code for the IP address.
// Returns "" (empty string) and nil error if the IP is not found or maps to ID 0.
// Returns ("", error) for database access errors or invalid IP format.
func (s *SxGeo) GetCountry(ip string) (string, error) {
	id, err := s.GetCountryID(ip)
	if err != nil {
		// Propagate lookup/parsing errors
		return "", fmt.Errorf("sxgo: failed to get country ID for IP %s: %w", ip, err)
	}
	if id == 0 {
		// ID 0 typically means not found or reserved range handled internally.
		return "", nil
	}
	iso := getISO(id) // Internal mapping lookup
	// Don't error if ID is valid but not in our map, just return ""
	return iso, nil
}

// GetCountryID retrieves the numeric country ID for the IP address.
// Returns 0 and nil error if the IP is not found or maps to ID 0.
// Returns (0, error) for database access errors or invalid IP format.
func (s *SxGeo) GetCountryID(ip string) (uint32, error) {
	seekOrID, err := s.getNum(ip) // Find the location ID or block seek position
	if err != nil {
		// Check if it's the specific "reserved range" error, which we treat as "not found" (ID 0)
		if errors.Is(err, errReservedRange) {
			return 0, nil
		}
		// Otherwise, propagate the error (invalid IP, DB read error, etc.)
		return 0, fmt.Errorf("sxgo: failed to get DB number for IP %s: %w", ip, err)
	}

	// If getNum returns 0 without error, it also indicates not found / handled internally.
	if seekOrID == 0 {
		return 0, nil
	}

	// If it's a City DB, the result (seekOrID) is a seek position into the city data.
	// We need to parse the city data to find the associated country ID.
	if s.header.maxCity > 0 {
		// Parse just enough to get the country ID. We don't need full details (false).
		// We only need the country ID stored within the city record itself.
		cityInfo, err := s.readData(seekOrID, s.header.maxCity, 2) // Type 2 for City
		if err != nil {
			// If parsing fails at this stage, it might indicate DB corruption or issues.
			return 0, fmt.Errorf("sxgo: failed to read city data for country ID lookup (seek %d) for IP %s: %w", seekOrID, ip, err)
		}
		if len(cityInfo) == 0 {
			// Should not happen if seekOrID was valid, but handle defensively.
			return 0, nil // No city info found, so no country ID.
		}
		// Extract country_id field defined in the pack format for cities.
		// Assumes the field name is 'country_id'.
		return uint32(getUint8(cityInfo, "country_id")), nil // Return 0 if field missing/invalid
	}

	// If it's a Country DB, the result from getNum is the country ID directly.
	return seekOrID, nil
}

// GetCity retrieves basic city and country information (ID, Lat, Lon, Names, Country ID/ISO).
// Region information is *not* included in this call. Use GetCityFull for region details.
// Returns (nil, nil) if the IP is not found, belongs to a reserved range, or if the database
// is not a City database (e.g., SxGeoCountry.dat).
// Returns (nil, error) for database access errors or invalid IP format.
func (s *SxGeo) GetCity(ip string) (*LocationInfo, error) {
	if s.header.maxCity == 0 {
		return nil, nil // Not a city database
	}
	seek, err := s.getNum(ip)
	if err != nil {
		if errors.Is(err, errReservedRange) {
			return nil, nil // Treat reserved range as not found
		}
		return nil, fmt.Errorf("sxgo: city lookup failed for IP %s: %w", ip, err)
	}
	if seek == 0 {
		return nil, nil // Not found or handled internally by getNum
	}

	// Parse city data, but request *not* full details (false)
	info, err := s.parseCity(seek, false)
	if err != nil {
		return nil, fmt.Errorf("sxgo: parsing city failed for IP %s (seek %d): %w", ip, seek, err)
	}
	// info might be nil if parsing failed internally despite no error return,
	// or if the specific seek pointed to empty/invalid data structure.
	return info, nil
}

// GetCityFull retrieves complete city, region, and country information.
// Returns (nil, nil) if the IP is not found, belongs to a reserved range, or if the database
// does not support city/region lookups (e.g., SxGeoCountry.dat).
// Returns (nil, error) for database access errors or invalid IP format.
func (s *SxGeo) GetCityFull(ip string) (*LocationInfo, error) {
	// Check if DB supports cities (which implies regions/countries conceptually)
	if s.header.maxCity == 0 {
		return nil, nil // Not a city/region capable database
	}
	// Check if region data exists and pack format is available (needed for full details)
	if s.header.maxRegion == 0 || len(s.packFormats) <= 1 || s.packFormats[1] == "" {
		// Cannot fulfill "Full" request if regions aren't present or parsable.
		// Fallback to GetCity? Or return error? Let's return error indicating inability.
		// Although GetCity might still work, the user explicitly asked for full details.
		// Alternatively, return partial data? Let's return what parseCity(seek, true) gives,
		// it handles missing region internally.
		// return nil, errors.New("sxgo: database lacks region data or format needed for GetCityFull")
	}

	seek, err := s.getNum(ip)
	if err != nil {
		if errors.Is(err, errReservedRange) {
			return nil, nil // Treat reserved range as not found
		}
		return nil, fmt.Errorf("sxgo: full city lookup failed for IP %s: %w", ip, err)
	}
	if seek == 0 {
		return nil, nil // Not found or handled internally by getNum
	}

	// Parse city data, requesting full details (true)
	info, err := s.parseCity(seek, true)
	if err != nil {
		return nil, fmt.Errorf("sxgo: parsing full city failed for IP %s (seek %d): %w", ip, seek, err)
	}
	return info, nil
}

// About returns metadata about the loaded Sypex Geo database.
func (s *SxGeo) About() map[string]interface{} {
	// Define known values based on SxGeo v2.2 documentation/common usage
	charsets := map[uint8]string{0: "utf-8", 1: "latin1", 2: "cp1251"}
	types := map[uint8]string{
		1: "SxGeo Country",
		2: "SxGeo City RU", 3: "SxGeo City EN", 4: "SxGeo City", // UTF?
		5: "SxGeo City Max RU", 6: "SxGeo City Max EN", 7: "SxGeo City Max", // UTF?
	}

	charset := "unknown"
	if cs, ok := charsets[s.header.charset]; ok {
		charset = cs
	}

	dbType := "unknown"
	if typ, ok := types[s.header.dbType]; ok {
		dbType = typ
	}

	createdTime := time.Unix(int64(s.header.timestamp), 0).UTC()

	return map[string]interface{}{
		"Created":              createdTime.Format("2006-01-02 15:04:05 MST"),
		"Timestamp":            s.header.timestamp,
		"Charset":              charset,
		"Type":                 dbType,
		"Version":              s.header.version,
		"Byte Index Entries":   s.header.byteIndexLen,
		"Main Index Entries":   s.header.mainIndexLen,
		"Blocks In Index Item": s.header.rangeBlocks,
		"IP Database Items":    s.header.dbItems,
		"ID Length (bytes)":    s.header.idLen,
		"DB Block Size":        s.blockSize,
		"Pack Format Strings":  s.packFormats, // Array of format strings
		"DB Begin Offset":      s.dbBegin,
		"Regions Begin Offset": s.regionsBegin,
		"Cities Begin Offset":  s.citiesBegin,
		"City Meta": map[string]interface{}{
			"Max Record Length": s.header.maxCity,
			"Total Data Size":   s.header.citySize,
		},
		"Region Meta": map[string]interface{}{
			"Max Record Length": s.header.maxRegion,
			"Total Data Size":   s.header.regionSize,
		},
		"Country Meta": map[string]interface{}{
			"Max Record Length": s.header.maxCountry,
			"Total Data Size":   s.header.countrySize, // Often 0 in v2.2 as country data is with cities
		},
	}
}
