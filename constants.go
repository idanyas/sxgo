package sxgo

// Operating Modes for initializing the SxGeo reader.
const (
	// ModeFile instructs the reader to work with the DB file directly,
	// reading required parts on demand. This uses less memory but is slower.
	ModeFile uint = 0

	// ModeMemory instructs the reader to load the entire database into memory
	// upon initialization. This uses more memory but provides the fastest lookups.
	// The file handle is closed after loading.
	ModeMemory uint = 1

	// ModeBatch can be combined with ModeMemory (e.g., ModeMemory | ModeBatch).
	// It pre-parses index data into arrays for potentially faster lookups,
	// especially when performing many lookups sequentially.
	ModeBatch uint = 2
)

// Internal constants
const (
	dbSig            = "SxG" // Sypex Geo signature
	dbHeaderLen      = 40    // Length of the database header
	dbBlockLenOffset = 3     // Offset of ID within a DB block (after 3 IP bytes)
)
