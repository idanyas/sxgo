package sxgo

import (
	"errors"
	"fmt"
	"io"
)

// readData reads and unpacks data (country, region, or city) from a given seek offset and max size.
// dataType: 0=country, 1=region, 2=city (indices into s.packFormats)
// seek: The offset relative to the beginning of the relevant data block (regionsBegin or citiesBegin).
// maxSize: The maximum number of bytes to read for this record.
// Returns the unpacked data as a map or an error.
// Internal function.
func (s *SxGeo) readData(seek uint32, maxSize uint16, dataType int) (map[string]interface{}, error) {
	// Validate data type and pack format existence
	if dataType < 0 || dataType >= len(s.packFormats) || s.packFormats[dataType] == "" {
		// Cannot unpack if format is missing. Return empty map, no error.
		return make(map[string]interface{}), nil
	}

	if maxSize == 0 {
		// Reading zero bytes is valid, results in empty map.
		return make(map[string]interface{}), nil
	}

	var data []byte // Byte slice containing the raw data for the record

	if s.memoryMode {
		var sourceData []byte // Reference to the full data block (regions or cities)
		var baseOffset int64  // Base offset (0 for relative seek in memory)

		switch dataType {
		case 0: // Country data (stored within the cities block in v2.2)
			sourceData = s.citiesData
			baseOffset = 0 // Seek is relative to start of citiesData
		case 1: // Region data
			sourceData = s.regionsData
			baseOffset = 0 // Seek is relative to start of regionsData
		case 2: // City data
			sourceData = s.citiesData
			baseOffset = 0 // Seek is relative to start of citiesData
		default:
			// Should be caught by earlier check
			return nil, fmt.Errorf("internal error: invalid data type %d in readData", dataType)
		}

		if sourceData == nil {
			// Data block for this type wasn't loaded or doesn't exist (e.g., no regions)
			return make(map[string]interface{}), nil // Return empty map, no error
		}

		sourceLen := int64(len(sourceData))
		start := baseOffset + int64(seek)
		end := start + int64(maxSize)

		// Bounds checks for memory read
		if start < 0 || start > sourceLen {
			// Invalid seek position
			return nil, fmt.Errorf("invalid seek %d (start %d) for data type %d in memory (source len %d)", seek, start, dataType, sourceLen)
		}
		// Clamp end to the actual length of the source data
		if end > sourceLen {
			end = sourceLen
		}
		// Check if the calculated slice is valid
		if start >= end {
			// Valid seek, but results in zero-length read (e.g., seek at end, or maxSize too large)
			return make(map[string]interface{}), nil
		}

		data = sourceData[start:end]

	} else { // File mode
		if s.f == nil {
			return nil, errors.New("file mode error: file handle is nil")
		}
		var absOffset int64 // Absolute offset in the .dat file

		switch dataType {
		case 0: // Country (relative to citiesBegin)
			absOffset = s.citiesBegin + int64(seek)
		case 1: // Region (relative to regionsBegin)
			absOffset = s.regionsBegin + int64(seek)
		case 2: // City (relative to citiesBegin)
			absOffset = s.citiesBegin + int64(seek)
		default:
			return nil, fmt.Errorf("internal error: invalid data type %d in readData", dataType)
		}

		readBytes := make([]byte, maxSize)
		n, err := s.f.ReadAt(readBytes, absOffset)

		// Handle read errors
		if err != nil && !errors.Is(err, io.EOF) {
			// Return only if it's not an expected EOF
			return nil, fmt.Errorf("failed to read data type %d at offset %d (seek %d): %w", dataType, absOffset, seek, err)
		}
		// If EOF or no error, proceed with the bytes read (n)
		if n == 0 {
			// Read 0 bytes, likely seek was at or past EOF.
			return make(map[string]interface{}), nil
		}
		data = readBytes[:n] // Use only the bytes actually read
	}

	// Unpack the retrieved data using the appropriate format string
	return unpack(s.packFormats[dataType], data) // unpack is defined in unpack.go
}

// parseCity retrieves and structures City, Region, and Country information.
// seek: The seek position pointing to the start of the City data record.
// full: If true, attempts to load Region details as well.
// Returns a LocationInfo struct or an error.
// Internal function.
func (s *SxGeo) parseCity(seek uint32, full bool) (*LocationInfo, error) {
	// Ensure pack formats exist for required types (at least city=2, country=0)
	requiredFormats := 3 // 0: Country, 1: Region, 2: City
	if len(s.packFormats) < requiredFormats {
		// If only country/city (len 2 or less), parsing full might fail.
		// Let readData handle missing formats individually.
		// return nil, fmt.Errorf("insufficient pack formats defined (need %d, have %d)", requiredFormats, len(s.packFormats))
	}
	if len(s.packFormats) <= 2 || s.packFormats[2] == "" {
		return nil, errors.New("database is missing city pack format")
	}
	// Country format (index 0) is also needed, checked later if accessed.

	info := &LocationInfo{}
	var cityData, regionData, countryData map[string]interface{}
	var err error

	// --- 1. Read City Data ---
	cityData, err = s.readData(seek, s.header.maxCity, 2) // Type 2 for City
	if err != nil {
		return nil, fmt.Errorf("failed to read city data at seek %d: %w", seek, err)
	}
	if len(cityData) == 0 {
		// If getNum returned a valid seek, but readData found nothing, the DB might be corrupt/incomplete.
		return nil, fmt.Errorf("city data not found or empty for seek %d", seek)
	}

	// Populate City struct from unpacked data
	info.City = &City{
		ID:     getUint32(cityData, "id"),
		Lat:    getFloat(cityData, "lat"),
		Lon:    getFloat(cityData, "lon"),
		NameRU: getString(cityData, "name_ru"),
		NameEN: getString(cityData, "name_en"),
		// Internal fields:
		regionSeek: getUint32(cityData, "region_seek"), // Store for later lookup if needed
		countryID:  getUint8(cityData, "country_id"),   // Store direct country ID as fallback
	}

	// --- 2. Read Region Data (if full=true and possible) ---
	regionSeek := info.City.regionSeek
	var countrySeek uint32 // Seek pointer found inside region data

	if full && regionSeek > 0 && s.header.maxRegion > 0 {
		// Check if region format exists (index 1)
		if len(s.packFormats) <= 1 || s.packFormats[1] == "" {
			// Cannot get region details without region format. Proceed without it.
			// Log this? Or ignore? Ignore for now.
		} else {
			regionData, err = s.readData(regionSeek, s.header.maxRegion, 1) // Type 1 for Region
			if err != nil {
				// Failed to read region, proceed without it, but maybe log?
				// return nil, fmt.Errorf("failed to read region data at seek %d: %w", regionSeek, err)
			} else if len(regionData) > 0 {
				info.Region = &Region{
					ID:     getUint32(regionData, "id"),
					NameRU: getString(regionData, "name_ru"),
					NameEN: getString(regionData, "name_en"),
					ISO:    getString(regionData, "iso"),
					// Internal field:
					countrySeek: getUint32(regionData, "country_seek"), // Store pointer from region
				}
				countrySeek = info.Region.countrySeek // Update countrySeek if region provided one
			}
			// If regionData was empty, info.Region remains nil.
		}
	}

	// --- 3. Read Country Data ---
	// Determine which country reference to use:
	// - Use countrySeek from Region if available (highest priority).
	// - Otherwise, use countryID from City record to look up country *by ID*.
	//   (SxGeo v2.2 stores country data mixed with city data, accessed via seek,
	//    but the structure might imply lookup by ID if no direct seek from region).
	// Let's assume country data is always read via a seek pointer, either from
	// the region record (countrySeek) or potentially calculated/known if it's
	// a direct country lookup (not handled here, assumes city context).
	// If countrySeek is 0, we must rely solely on the countryID from the city record.

	countryIDToUse := info.City.countryID // Default to ID from city record

	if countrySeek > 0 && s.header.maxCountry > 0 {
		// We have a specific seek pointer from the region data.
		// Check if country format exists (index 0)
		if len(s.packFormats) == 0 || s.packFormats[0] == "" {
			// Cannot read country data without format. Rely on city's countryID below.
		} else {
			countryData, err = s.readData(countrySeek, s.header.maxCountry, 0) // Type 0 for Country
			if err != nil {
				// Failed to read country, proceed using city's countryID, maybe log?
				// return nil, fmt.Errorf("failed to read country data via region at seek %d: %w", countrySeek, err)
			}
			// If read successful, update the ID from the data itself if available
			if len(countryData) > 0 {
				// Verify if countryData contains an 'id' field
				if _, exists := countryData["id"]; exists {
					countryIDToUse = getUint8(countryData, "id") // Use ID from unpacked country data
				}
				// If 'id' field doesn't exist in country pack format, stick with city's countryID?
				// Let's assume the format includes 'id'.
			}
			// If countryData was empty, fallback to city's countryID below.
		}
	}

	// --- 4. Populate Country Struct ---
	if countryIDToUse > 0 {
		// We have a country ID (either from city or updated from country data read via seek).
		isoCode := getISO(uint32(countryIDToUse)) // Get ISO code from internal map

		// If we successfully read full country data via seek:
		if len(countryData) > 0 {
			info.Country = &Country{
				ID:     countryIDToUse, // Use the ID (potentially updated)
				ISO:    isoCode,
				Lat:    getFloat(countryData, "lat"),
				Lon:    getFloat(countryData, "lon"),
				NameRU: getString(countryData, "name_ru"),
				NameEN: getString(countryData, "name_en"),
			}
		} else {
			// If we didn't read full country data (no seek, read failed, or format missing),
			// create a minimal Country struct using only the ID (from city) and ISO code.
			info.Country = &Country{
				ID:  countryIDToUse,
				ISO: isoCode,
				// Lat/Lon/Names will be zero/empty
			}
		}
	}
	// If countryIDToUse was 0, info.Country remains nil.

	// Final check: Ensure we have at least *some* data if city read succeeded.
	if info.City == nil && info.Region == nil && info.Country == nil {
		// This shouldn't happen if cityData read succeeded initially.
		return nil, errors.New("internal error: failed to retrieve any location information after parsing")
	}

	return info, nil
}
