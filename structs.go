package sxgo

// LocationInfo holds the combined geolocation information for an IP address.
// Depending on the lookup method (GetCity, GetCityFull) and the database contents,
// some fields might be nil.
type LocationInfo struct {
	City    *City    `json:"city,omitempty"`    // City details, nil if not found or not requested.
	Region  *Region  `json:"region,omitempty"`  // Region details, nil if not found or not requested via GetCityFull.
	Country *Country `json:"country,omitempty"` // Country details, nil if not found.
}

// City information.
type City struct {
	ID     uint32  `json:"id"`                // City ID in the database.
	Lat    float64 `json:"lat"`               // Latitude.
	Lon    float64 `json:"lon"`               // Longitude.
	NameRU string  `json:"name_ru,omitempty"` // City name in Russian (if available).
	NameEN string  `json:"name_en,omitempty"` // City name in English (if available).

	// Internal fields, not part of public API or JSON output
	regionSeek uint32 // Seek position for the region data.
	countryID  uint8  // Country ID associated directly with this city (fallback).
}

// Region information.
type Region struct {
	ID     uint32 `json:"id"`                // Region ID in the database.
	NameRU string `json:"name_ru,omitempty"` // Region name in Russian (if available).
	NameEN string `json:"name_en,omitempty"` // Region name in English (if available).
	ISO    string `json:"iso,omitempty"`     // ISO 3166-2 region code (e.g., "US-CA").

	// Internal field, not part of public API or JSON output
	countrySeek uint32 // Seek position for the country data.
}

// Country information.
type Country struct {
	ID     uint8   `json:"id"`                // Country ID in the database.
	ISO    string  `json:"iso"`               // ISO 3166-1 alpha-2 country code (e.g., "US").
	Lat    float64 `json:"lat"`               // Latitude (often centroid).
	Lon    float64 `json:"lon"`               // Longitude (often centroid).
	NameRU string  `json:"name_ru,omitempty"` // Country name in Russian (if available).
	NameEN string  `json:"name_en,omitempty"` // Country name in English (if available).
	// Timezone string  `json:"timezone,omitempty"` // Timezone information is not typically included in the base SxGeo City format handled here.
}
