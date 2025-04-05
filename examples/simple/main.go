package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/idanyas/sxgo" // Import the module
)

func main() {
	// --- Configuration ---
	// Ensure you have downloaded the SxGeoCity.dat file
	// from http://sypexgeo.net/ru/download/
	dbFile := "SxGeoCity.dat"
	ipToLookup := "93.158.134.3" // Example IP (Yandex)
	// ipToLookup := "8.8.8.8" // Example IP (Google DNS)

	// --- File Check ---
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		log.Fatalf("Database file not found: %s\nPlease download the SxGeoCity database in binary format.", dbFile)
	}

	// --- Initialize SxGeo ---
	// Choose Mode:
	// - sxgo.ModeFile   (Low memory, reads from disk)
	// - sxgo.ModeMemory (Fastest, loads all to RAM)
	// - Combine with sxgo.ModeBatch for index optimization: sxgo.ModeMemory | sxgo.ModeBatch
	mode := sxgo.ModeMemory
	geo, err := sxgo.New(dbFile, mode)
	if err != nil {
		log.Fatalf("Error initializing SypexGeo (Mode: %d): %v", mode, err)
	}
	// Defer Close() - crucial for ModeFile to release handle, safe for ModeMemory
	defer geo.Close()

	log.Printf("Sypex Geo database '%s' initialized successfully.", dbFile)

	// --- Perform Lookup ---
	log.Printf("Looking up IP: %s", ipToLookup)

	// Use GetCityFull for complete details (City, Region, Country)
	location, err := geo.GetCityFull(ipToLookup)
	if err != nil {
		// This catches fundamental errors like invalid IP format or DB read issues
		log.Printf("Error looking up location for %s: %v", ipToLookup, err)
		return
	}

	// --- Process Result ---
	if location == nil {
		// This indicates the IP was not found in the database ranges
		// or might belong to a reserved range excluded from lookup.
		log.Printf("No location information found for IP %s.", ipToLookup)
		return
	}

	// Print the found location as pretty JSON
	jsonData, err := json.MarshalIndent(location, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling location data to JSON: %v", err)
	}
	fmt.Println(string(jsonData))

	// --- Optional: Get DB Info ---
	// aboutInfo := geo.About()
	// aboutJson, _ := json.MarshalIndent(aboutInfo, "", "  ")
	// fmt.Printf("\n--- Database Info ---\n%s\n", string(aboutJson))
}
