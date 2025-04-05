package sxgo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Special error for reserved ranges, treated internally as "not found".
var errReservedRange = errors.New("IP address is in a reserved or local range")

// getNum finds the internal ID (for country DB) or seek position (for city DB)
// for a given IP address.
// Returns 0 and potentially errReservedRange if IP is local/reserved.
// Returns 0 and other error for invalid IP format or DB read issues.
// Internal function.
func (s *SxGeo) getNum(ipStr string) (uint32, error) {
	ipNum, ok := ip2long(ipStr)
	if !ok {
		return 0, fmt.Errorf("invalid IPv4 address: %q", ipStr)
	}

	ipBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ipBytes, ipNum)
	ip1 := uint32(ipBytes[0]) // First byte

	// --- First Byte Index Lookup ---
	// Handle reserved/local ranges (similar to original PHP logic)
	// Check 0.x.x.x, 10.x.x.x, 127.x.x.x
	// Also check if ip1 is beyond the bounds of the byte index.
	byteIndexLen := uint32(s.header.byteIndexLen)
	if ip1 == 0 || ip1 == 10 || ip1 == 127 || ip1 >= byteIndexLen {
		// Return a specific error that callers can check if needed,
		// otherwise treat as "not found" (return 0, nil in public methods).
		return 0, errReservedRange
	}

	// Find block range using the first byte index
	var minBlock, maxBlock uint32
	useParsedIndexes := s.batchMode || s.memoryMode

	if useParsedIndexes { // Use pre-parsed array
		// Bounds check: ip1 is >= 1 and < byteIndexLen here
		minBlock = s.byteIndexArr[ip1-1] // Index for previous byte determines start
		maxBlock = s.byteIndexArr[ip1]   // Index for this byte determines end
	} else { // Use raw byte string
		// Bounds already checked conceptually (ip1 < byteIndexLen)
		// ip1 > 0 enforced by initial check
		minOffset := (ip1 - 1) * 4
		maxOffset := ip1 * 4
		minBlock = binary.BigEndian.Uint32(s.byteIndexStr[minOffset : minOffset+4])
		maxBlock = binary.BigEndian.Uint32(s.byteIndexStr[maxOffset : maxOffset+4])
	}

	// --- Main Index Search (if range is large) ---
	var searchMin, searchMax uint32
	rangeBlocks := uint32(s.header.rangeBlocks) // Cast to uint32 for calculations

	if maxBlock-minBlock > rangeBlocks {
		// Range is large, use main index to narrow down
		if rangeBlocks == 0 {
			// Should be caught by header validation, but safeguard
			return 0, errors.New("database header range is zero, cannot search main index")
		}

		// Calculate range within the main index array/string
		// Note: PHP logic used floor(block / range). Integer division achieves this.
		mainIdxMin := minBlock / rangeBlocks
		// PHP: ceil(maxBlock / range) - 1 => floor((maxBlock-1) / range) ?
		// Let's try (maxBlock - 1) / rangeBlocks which seems equivalent for positive numbers
		mainIdxMax := (maxBlock - 1) / rangeBlocks

		// Ensure mainIdxMax is not less than mainIdxMin
		if mainIdxMax < mainIdxMin {
			mainIdxMax = mainIdxMin // Can happen if maxBlock is just inside the next range
		}

		// Search the main index to find the relevant partition
		part := s.searchIdx(ipBytes, mainIdxMin, mainIdxMax) // `part` is the index in mainIndexArr/Str

		// Calculate the search range in the main DB based on the main index result
		if part == 0 {
			searchMin = 0 // Should start from the beginning of the section? Or minBlock? Let's use minBlock.
			searchMin = minBlock
		} else {
			// The `part` index points to the *start* of the block range.
			// The search should start from the block indicated by `part`'s value * range.
			// Let's re-evaluate PHP logic. `search_idx` returns the index `min` where `ip > main_index[min-1]` and `ip <= main_index[min]`
			// It seems `part` corresponds to the block *containing* or *just after* the IP.
			// search_min = $this->main_index[$part-1] * $this->header['range'] ??? No, that uses the IP value.
			// PHP sets: $min = $part ? $this->main_index[$part-1] : 0;
			// $max = $this->main_index[$part];
			// Wait, PHP searchIdx returns index `i` such that `ip >= block[i]`
			// And searchDb uses `min = part * range`, `max = (part+1) * range`
			// Let's stick to that:
			searchMin = part * rangeBlocks
		}

		// Max range: Use part+1, clamped by total DB items
		if part >= uint32(s.header.mainIndexLen) { // If part points past the end of main index
			searchMax = s.header.dbItems
		} else {
			searchMax = (part + 1) * rangeBlocks
		}

		// Ensure search range stays within the initial bounds derived from the first byte index
		if searchMin < minBlock {
			searchMin = minBlock
		}
		if searchMax > maxBlock {
			searchMax = maxBlock
		}

	} else {
		// Range is small enough, search directly within the first byte's block range
		searchMin = minBlock
		searchMax = maxBlock
	}

	// --- Final DB Block Search ---

	// Final checks on search range before searching DB blocks
	if searchMin >= searchMax {
		// This can happen in edge cases. If searchMin points to a valid block, search just that one.
		// Or if the IP is smaller than the first block in the range.
		// PHP's `search_db` handles `min == max` by returning the ID of `db[min-1]`
		// Let's try searching the block `searchMin-1` if `searchMin > 0`, otherwise fail.
		// However, `searchDb` function expects a range `min` to `max` (exclusive?).
		// If min==max, it implies the IP might match the block *before* min.
		// Let's adjust searchDb to handle this or return error here.
		// For now, let's ensure `max` is at least `min + 1` if `min` is valid.
		if searchMin < s.header.dbItems {
			searchMax = searchMin + 1 // Search the single block at searchMin
		} else {
			// searchMin is already out of bounds.
			// This might indicate the IP is larger than any in the DB.
			// The searchDb logic might handle this by returning the last ID.
			// Let's try searching the last valid block if searchMin points beyond the end.
			if s.header.dbItems > 0 {
				searchMin = s.header.dbItems - 1
				searchMax = s.header.dbItems // searchDb range is [min, max)
			} else {
				return 0, fmt.Errorf("search range invalid (searchMin %d >= searchMax %d) and DB is empty", searchMin, searchMax)
			}
			// return 0, fmt.Errorf("search range invalid (searchMin %d >= searchMax %d)", searchMin, searchMax)
		}
	}
	// Ensure searchMax does not exceed total items
	if searchMax > s.header.dbItems {
		searchMax = s.header.dbItems
	}

	// Perform the search within the final DB block range
	var dbPartToSearch []byte
	var searchOffset uint32 // Relative offset for searchDb if reading a partial block

	if s.memoryMode {
		if s.dbData == nil {
			return 0, errors.New("cannot search: dbData not loaded in memory mode")
		}
		// Provide the relevant slice of the full dbData
		startByte := int64(searchMin) * int64(s.blockSize)
		// Ensure endByte doesn't exceed available data
		endByte := int64(searchMax) * int64(s.blockSize)
		if endByte > int64(len(s.dbData)) {
			endByte = int64(len(s.dbData))
		}
		// Handle case where startByte might be >= endByte (e.g., searching beyond end)
		if startByte >= endByte {
			if s.header.dbItems > 0 {
				// Try to return the ID of the very last block
				lastBlockStart := int64(s.header.dbItems-1) * int64(s.blockSize)
				idOffset := lastBlockStart + int64(dbBlockLenOffset)
				if idOffset+int64(s.header.idLen) <= int64(len(s.dbData)) {
					return s.decodeID(s.dbData[idOffset : idOffset+int64(s.header.idLen)])
				}
			}
			return 0, fmt.Errorf("invalid memory search range calculated: start %d >= end %d", startByte, endByte)
		}

		dbPartToSearch = s.dbData[startByte:endByte]
		searchOffset = 0 // Search relative to the start of this slice

	} else { // File mode: read the relevant part of the DB file
		readCount := searchMax - searchMin
		if readCount == 0 {
			// This case should ideally be handled by the searchMin >= searchMax logic above.
			// If we reach here, something is inconsistent.
			return 0, errors.New("calculated file search range has zero items unexpectedly")
			// Try reading the block *before* searchMin?
			// if searchMin > 0 {
			// 	searchMin--
			// 	readCount = 1
			// } else {
			// 	return 0, errors.New("calculated file search range has zero items at start")
			// }
		}

		readLen := int64(readCount) * int64(s.blockSize)
		readOffset := s.dbBegin + int64(searchMin)*int64(s.blockSize)

		if s.f == nil {
			return 0, errors.New("cannot read file: file handle is nil (must be in memory mode but dbData is missing?)")
		}

		dbPart := make([]byte, readLen)
		n, err := s.f.ReadAt(dbPart, readOffset)

		// Handle read errors, especially EOF
		if err != nil && !errors.Is(err, io.EOF) {
			// Real read error
			return 0, fmt.Errorf("failed to read DB part at offset %d (len %d): %w", readOffset, readLen, err)
		}
		// If EOF occurred, or no error, proceed with the bytes read (n).
		// It's okay if n < readLen, especially if reading the last blocks.
		if n == 0 {
			// Read 0 bytes. Offset might be beyond EOF, or readLen was 0.
			// If we expected to read data (readLen > 0), this is an issue.
			if readLen > 0 {
				// Could indicate IP is larger than anything in DB. What's the correct ID? Last one?
				// Let's try getting the last ID. Need to read the last block.
				if s.header.dbItems > 0 {
					lastBlockOffset := s.dbBegin + int64(s.header.dbItems-1)*int64(s.blockSize)
					lastBlockBytes := make([]byte, s.blockSize)
					m, readErr := s.f.ReadAt(lastBlockBytes, lastBlockOffset)
					if readErr == nil && m >= int(dbBlockLenOffset+s.header.idLen) {
						return s.decodeID(lastBlockBytes[dbBlockLenOffset : dbBlockLenOffset+s.header.idLen])
					}
				}
				// Fallback error if getting last ID failed or DB empty
				return 0, fmt.Errorf("read 0 bytes at offset %d (EOF or bad range)", readOffset)
			}
			// If readLen was 0, then maybe okay, searchDb should handle empty input.
		}
		dbPartToSearch = dbPart[:n] // Use only the bytes actually read
		searchOffset = 0            // Search relative to the start of dbPartToSearch
	}

	// Perform the binary search on the retrieved data slice
	return s.searchDb(dbPartToSearch, ipBytes, searchOffset, uint32(len(dbPartToSearch)/int(s.blockSize)))
}

// searchIdx performs binary search on the main index (array or raw bytes).
// ipBytes: 4-byte IP representation.
// min, max: index range within mainIndexArr/mainIndexStr to search.
// Returns the index `p` such that the IP belongs in the DB block range defined by `p`.
// Internal function.
func (s *SxGeo) searchIdx(ipBytes []byte, min, max uint32) uint32 {
	ipNumSearch := binary.BigEndian.Uint32(ipBytes) // Use full IP for comparison in main index
	useParsedIndexes := s.batchMode || s.memoryMode
	var currentMax uint32 // Store the actual upper bound used in search

	if useParsedIndexes { // Use array
		indexLen := uint32(len(s.mainIndexArr))
		if indexLen == 0 {
			return min // Or 0? Return min seems safer if range was [min, max)
		}
		// Clamp max to the actual bounds of the array
		if max >= indexLen {
			max = indexLen - 1
		}
		currentMax = max // Store the adjusted max
		if min > currentMax {
			// If initial range was invalid or clamped max < min, return min
			return min
		}

		for max-min > 8 { // Binary search phase
			mid := min + (max-min)/2 // Avoid overflow
			if ipNumSearch > s.mainIndexArr[mid] {
				min = mid + 1 // Standard binary search logic
			} else {
				max = mid // Keep mid in the upper range
			}
		}

		// Linear scan for the last few elements
		// Find first element >= ipNumSearch
		for min <= currentMax { // Use clamped max
			if ipNumSearch > s.mainIndexArr[min] {
				min++
			} else {
				break // Found the first element >= ipNumSearch
			}
		}
	} else { // Use raw bytes
		indexLenBytes := uint32(len(s.mainIndexStr))
		if indexLenBytes == 0 {
			return min
		}
		indexLen := indexLenBytes / 4 // Number of uint32 entries
		if indexLen == 0 {
			return min
		}
		// Clamp max to the actual bounds of the index data
		if max >= indexLen {
			max = indexLen - 1
		}
		currentMax = max
		if min > currentMax {
			return min
		}

		for max-min > 8 { // Binary search phase
			mid := min + (max-min)/2
			offset := mid * 4
			// Bounds check should be implicitly handled by max clamping, but double check theory
			// If mid can reach currentMax, offset is max*4. Need offset+4 <= indexLenBytes
			// max*4+4 <= indexLenBytes => (indexLen-1)*4+4 <= indexLenBytes => indexLen*4 <= indexLenBytes. Correct.
			idxVal := binary.BigEndian.Uint32(s.mainIndexStr[offset : offset+4])
			if ipNumSearch > idxVal {
				min = mid + 1
			} else {
				max = mid
			}
		}

		// Linear scan phase
		for min <= currentMax { // Use clamped max
			offset := min * 4
			// Check offset before reading (though clamping should prevent exceeding len)
			if offset+4 > indexLenBytes {
				break // Should not happen if logic is correct
			}
			idxVal := binary.BigEndian.Uint32(s.mainIndexStr[offset : offset+4])
			if ipNumSearch > idxVal {
				min++
			} else {
				break // Found first element >= ipNumSearch
			}
		}
	}

	// `min` now points to the first index `p` in the main index where `mainIndex[p] >= ipNumSearch`.
	// The PHP logic seems to relate this `min` value directly to the range calculation in getNum:
	// `$p = $this->search_idx($ipn, 0, count($this->m_idx) - 1);`
	// `$min = $p ? $this->m_idx[$p-1] : 0;` // Uses value at index p-1?
	// `$max = $this->m_idx[$p];` // Uses value at index p?
	// No, later PHP uses: `$min_s = $part * $this->header['range']`
	// So, the *index* `min` found here is the `part` needed.
	return min
}

// searchDb performs binary search within a slice of DB blocks (`data`).
// data: byte slice containing the DB blocks for the relevant range.
// ipBytes: 4-byte representation of the IP to search for.
// min, max: *relative* block indices within the `data` slice to search [min, max).
// Returns the location ID (seek for city, ID for country) found for the IP.
// Internal function.
func (s *SxGeo) searchDb(data []byte, ipBytes []byte, min, max uint32) (uint32, error) {
	// Use only the last 3 bytes of the IP for comparison within DB blocks
	ipSuffix := ipBytes[1:]
	blockSize := s.blockSize // Local copy for convenience
	dataLen := uint32(len(data))

	// Basic validation: need enough data for at least one block comparison?
	// Allow empty data? If data is empty, max should be 0.
	if dataLen == 0 {
		if max > min { // If the range expected data, return error
			return 0, errors.New("searchDb called with empty data but non-empty range")
		}
		// If range was also empty [0, 0), maybe return 0 ID?
		return 0, nil // No data, no ID found
	}

	// Ensure max is within the bounds of the actual data provided
	numBlocksInData := dataLen / blockSize
	if max > numBlocksInData {
		max = numBlocksInData // Adjust max to the number of full blocks available
	}

	// Handle invalid range after adjustment (e.g., min >= max)
	if min >= max {
		// This means the search range is empty or invalid.
		// PHP logic returns the ID from the block *before* `min`.
		if min > 0 {
			targetBlockIdx := int64(min - 1) // Index of the block before the invalid range start
			idOffset := (targetBlockIdx * int64(blockSize)) + int64(dbBlockLenOffset)
			idEndOffset := idOffset + int64(s.header.idLen)

			// Check if this ID is within the bounds of the *original* data slice
			if idOffset >= 0 && uint32(idEndOffset) <= dataLen {
				return s.decodeID(data[idOffset:idEndOffset])
			}
		}
		// If min was 0 or reading block min-1 failed, return "not found" (0) or error?
		// Return 0 for not found seems consistent.
		// return 0, fmt.Errorf("invalid search range in searchDb [min %d, max %d) after bounds check", min, max)
		return 0, nil // Indicate not found within the provided data/range
	}

	// Store original min for edge case handling later
	// origMin := min

	// --- Binary Search Phase ---
	currentMax := max // Use adjusted max for loop condition
	for max-min > 8 {
		mid := min + (max-min)/2 // Avoid overflow
		blockOffset := int64(mid) * int64(blockSize)
		// blockOffset + dbBlockLenOffset will be < dataLen because mid < max <= numBlocksInData

		blockIP := data[blockOffset : blockOffset+int64(dbBlockLenOffset)] // Get 3 bytes IP part

		// Compare ipSuffix with blockIP (byte by byte)
		cmp := 0
		for i := 0; i < dbBlockLenOffset; i++ {
			if ipSuffix[i] > blockIP[i] {
				cmp = 1
				break
			}
			if ipSuffix[i] < blockIP[i] {
				cmp = -1
				break
			}
		}

		if cmp > 0 { // ipSuffix > blockIP
			min = mid + 1 // Standard binary search: exclude mid, search upper half
		} else {
			max = mid // ipSuffix <= blockIP: include mid, search lower half
		}
	}

	// --- Linear Scan Phase ---
	// `min` is now the start of the small range to scan
	// `max` (from binary search) is the end of the small range (exclusive?) - Let's use currentMax
	// Scan from `min` up to `currentMax` (exclusive of currentMax)

	// Find the first block where blockIP > ipSuffix
	for min < currentMax { // Check blocks from index `min` up to `currentMax - 1`
		blockOffset := int64(min) * int64(blockSize)
		// blockOffset + dbBlockLenOffset < dataLen because min < currentMax <= numBlocksInData

		blockIP := data[blockOffset : blockOffset+int64(dbBlockLenOffset)]

		// Compare ipSuffix with blockIP
		cmp := 0
		for i := 0; i < dbBlockLenOffset; i++ {
			if ipSuffix[i] > blockIP[i] {
				cmp = 1
				break
			}
			if ipSuffix[i] < blockIP[i] {
				cmp = -1
				break
			}
		}

		if cmp < 0 { // ipSuffix < blockIP. We found the block *after* the one we need.
			break
		}
		// if cmp >= 0 (ipSuffix >= blockIP), continue searching.
		min++
	}

	// `min` now points to the first block whose IP is strictly greater than the search IP suffix,
	// OR it points to `currentMax` if the search IP was >= all blocks checked in the linear scan.
	// The correct ID is in the block *before* this `min`.
	targetBlockIdx := int64(min - 1)

	// Handle edge case: If `min` never advanced from `origMin`, it means the search IP
	// was smaller than the IP of the very first block (`origMin`) in the search range.
	// In this case, PHP's logic effectively returns the ID of the block *before* `origMin`.
	// `targetBlockIdx` will be `origMin - 1`.

	// Check if targetBlockIdx is valid (>= 0)
	if targetBlockIdx < 0 {
		// This implies the IP is smaller than the first block in the *entire searched segment*.
		// This could happen if the range was [0, ...] and IP < block[0].
		// Or if the range started mid-DB, and IP < block[origMin].
		// What should we return? ID 0 (not found)?
		return 0, nil // Consistent with "not found"
	}

	// Calculate offset for the ID within the target block.
	idOffset := (targetBlockIdx * int64(blockSize)) + int64(dbBlockLenOffset)
	idEndOffset := idOffset + int64(s.header.idLen)

	// Final boundary check on the calculated offsets against the input `data` slice length.
	if uint32(idEndOffset) > dataLen {
		// This could happen if targetBlockIdx points to the very last block, but the `data`
		// slice was somehow truncated or didn't contain the full block.
		// Or if targetBlockIdx calculation somehow went wrong.
		// Try returning ID 0?
		// return 0, fmt.Errorf("calculated ID offset %d-%d is out of data bounds %d (target block %d)", idOffset, idEndOffset, dataLen, targetBlockIdx)
		return 0, nil // Indicate not found / data truncation issue
	}

	// Decode and return the ID
	return s.decodeID(data[idOffset:idEndOffset])
}
