// Package sxgo provides functionality to read and query Sypex Geo databases (v2.2).
//
// It allows looking up IP addresses to find geographical information such as
// city, region, and country details, including coordinates and ISO codes.
//
// The package supports different operating modes:
//   - ModeFile: Reads directly from the .dat file on each lookup (lower memory, slower).
//   - ModeMemory: Loads the entire database into RAM for fast lookups (higher memory).
//   - ModeBatch: Pre-parses indexes for potentially faster batch processing (used with ModeMemory).
//
// Basic Usage:
//
//	// Ensure SxGeoCity.dat is available
//	geo, err := sxgo.New("SxGeoCity.dat", sxgo.ModeMemory)
//	if err != nil {
//	    log.Fatalf("Failed to initialize SypexGeo: %v", err)
//	}
//	defer geo.Close() // Important if not using ModeMemory
//
//	location, err := geo.GetCityFull("93.158.134.3") // Example IP
//	if err != nil {
//	    log.Printf("Error looking up IP: %v", err)
//	} else if location != nil {
//	    // Process location data (e.g., print as JSON)
//	    jsonData, _ := json.MarshalIndent(location, "", "  ")
//	    fmt.Println(string(jsonData))
//	} else {
//	    fmt.Println("Location not found for IP.")
//	}
package sxgo
