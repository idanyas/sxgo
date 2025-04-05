# sxgo - Sypex Geo Go Reader

`sxgo` is a Go library for reading IP address geolocation information from Sypex Geo database files (`.dat` format, v2.2). It allows you to look up details like country, region, city, latitude, and longitude for a given IPv4 address.

## Features

*   Supports Sypex Geo v2.2 database format (`SxGeoCity.dat`, `SxGeoCountry.dat`).
*   Provides lookups for Country, Region, and City information (depending on the database used).
*   Includes latitude, longitude, and ISO codes.
*   Multiple operating modes:
    *   `ModeFile`: Reads from disk on demand (low memory, slower).
    *   `ModeMemory`: Loads the entire database into RAM (high performance, higher memory).
    *   `ModeBatch`: Optimizes index lookups, useful with `ModeMemory` for high throughput.
*   Simple API.

## Installation

```bash
go get github.com/idanyas/sxgo
```

## Database File

You need to download a Sypex Geo database file separately. The free City database is commonly used:

1.  Go to the [Sypex Geo Download Page](http://sypexgeo.net/ru/download/) (or the English version if available).
2.  Download the **SxGeoCity** database in the **binary (SxGeo) format**. It will likely be named `SxGeoCity.dat`.
3.  Place the downloaded `.dat` file where your application can access it.

## Usage

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/idanyas/sxgo" // Import the module
)

func main() {
	dbFile := "SxGeoCity.dat" // Path to your downloaded Sypex Geo database file
	ipToLookup := "93.158.134.3" // Example IP (Yandex)

	// Check if DB file exists
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		log.Fatalf("Database file not found: %s. Download from http://sypexgeo.net/ru/download/", dbFile)
	}

	// --- Initialize SxGeo ---
	// Choose a mode: ModeMemory is generally recommended for performance.
	// Use ModeFile if memory usage is a primary concern.
	// ModeBatch can be combined: sxgo.ModeMemory | sxgo.ModeBatch
	geo, err := sxgo.New(dbFile, sxgo.ModeMemory)
	if err != nil {
		log.Fatalf("Error initializing SypexGeo: %v", err)
	}
	// Defer closing the file handle (only relevant for ModeFile, but safe to call always)
	defer geo.Close()

	fmt.Printf("Sypex Geo database initialized successfully (Mode: Memory)\n")
	fmt.Printf("Looking up IP: %s\n\n", ipToLookup)

	// --- Perform Lookup ---

	// GetCityFull retrieves City, Region, and Country info
	location, err := geo.GetCityFull(ipToLookup)
	if err != nil {
		fmt.Printf("Error getting location info for %s: %v\n", ipToLookup, err)
		return
	}

	// Check if location was found (nil if not found or IP is reserved)
	if location == nil {
		fmt.Printf("No location information found for IP %s\n", ipToLookup)
		return
	}

	// Print the result as pretty JSON
	jsonData, err := json.MarshalIndent(location, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling location to JSON: %v", err)
	}
	fmt.Println(string(jsonData))

	// --- Other Lookup Examples ---

	// GetCity (only City and Country, no Region)
	// cityInfo, _ := geo.GetCity(ipToLookup)
	// if cityInfo != nil {
	//     fmt.Printf("\nCity Info Only: %+v\n", cityInfo.City)
	//     fmt.Printf("Country Info Only: %+v\n", cityInfo.Country)
	// }

	// GetCountryID
	// countryID, _ := geo.GetCountryID(ipToLookup)
	// fmt.Printf("\nCountry ID: %d\n", countryID)

	// GetCountry (ISO Code)
	// countryISO, _ := geo.GetCountry(ipToLookup)
	// fmt.Printf("Country ISO: %s\n", countryISO)

	// --- Database Metadata ---
	// about := geo.About()
	// fmt.Printf("\nDatabase Info:\n")
	// aboutJSON, _ := json.MarshalIndent(about, "", "  ")
	// fmt.Println(string(aboutJSON))
}

```

## API Overview

*   `sxgo.New(dbFile string, mode uint) (*SxGeo, error)`: Creates a new SxGeo reader instance.
*   `(*SxGeo).Close() error`: Releases resources (closes file handle in ModeFile).
*   `(*SxGeo).GetCityFull(ip string) (*LocationInfo, error)`: Gets full City, Region, Country details.
*   `(*SxGeo).GetCity(ip string) (*LocationInfo, error)`: Gets City and Country details (Region will be nil).
*   `(*SxGeo).GetCountry(ip string) (string, error)`: Gets the two-letter ISO country code.
*   `(*SxGeo).GetCountryID(ip string) (uint32, error)`: Gets the numeric country ID.
*   `(*SxGeo).Get(ip string) (interface{}, error)`: Generic lookup returning `*LocationInfo` for City DBs or `string` (ISO code) for Country DBs. Use specific methods for type safety.
*   `(*SxGeo).About() map[string]interface{}`: Returns metadata about the loaded database.
