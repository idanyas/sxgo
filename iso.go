package sxgo

// id2iso maps country ID (index) to its two-letter ISO 3166-1 alpha-2 code.
// Index 0 is unused or represents an unknown/unspecified location.
// Based on common SxGeo distribution.
var id2iso = []string{
	"", "AP", "EU", "AD", "AE", "AF", "AG", "AI", "AL", "AM", "CW", // 10 (CW CURACAO new) old: AN NETHERLANDS ANTILLES
	"AO", "AQ", "AR", "AS", "AT", "AU", "AW", "AZ", "BA", "BB", "BD", // 21
	"BE", "BF", "BG", "BH", "BI", "BJ", "BM", "BN", "BO", "BR", "BS", // 32
	"BT", "BV", "BW", "BY", "BZ", "CA", "CC", "CD", "CF", "CG", "CH", // 43
	"CI", "CK", "CL", "CM", "CN", "CO", "CR", "CU", "CV", "CX", "CY", // 54
	"CZ", "DE", "DJ", "DK", "DM", "DO", "DZ", "EC", "EE", "EG", "EH", // 65
	"ER", "ES", "ET", "FI", "FJ", "FK", "FM", "FO", "FR", "SX", "GA", // 76 (SX SINT MAARTEN new) old: FX FRANCE, METROPOLITAN
	"GB", "GD", "GE", "GF", "GH", "GI", "GL", "GM", "GN", "GP", "GQ", // 87
	"GR", "GS", "GT", "GU", "GW", "GY", "HK", "HM", "HN", "HR", "HT", // 98
	"HU", "ID", "IE", "IL", "IN", "IO", "IQ", "IR", "IS", "IT", "JM", // 109
	"JO", "JP", "KE", "KG", "KH", "KI", "KM", "KN", "KP", "KR", "KW", // 120
	"KY", "KZ", "LA", "LB", "LC", "LI", "LK", "LR", "LS", "LT", "LU", // 131
	"LV", "LY", "MA", "MC", "MD", "MG", "MH", "MK", "ML", "MM", "MN", // 142
	"MO", "MP", "MQ", "MR", "MS", "MT", "MU", "MV", "MW", "MX", "MY", // 153
	"MZ", "NA", "NC", "NE", "NF", "NG", "NI", "NL", "NO", "NP", "NR", // 164
	"NU", "NZ", "OM", "PA", "PE", "PF", "PG", "PH", "PK", "PL", "PM", // 175
	"PN", "PR", "PS", "PT", "PW", "PY", "QA", "RE", "RO", "RU", "RW", // 186
	"SA", "SB", "SC", "SD", "SE", "SG", "SH", "SI", "SJ", "SK", "SL", // 197
	"SM", "SN", "SO", "SR", "ST", "SV", "SY", "SZ", "TC", "TD", "TF", // 208
	"TG", "TH", "TJ", "TK", "TM", "TN", "TO", "TL", "TR", "TT", "TV", // 219 (TL TIMOR-LESTE new) old: TP EAST TIMOR
	"TW", "TZ", "UA", "UG", "UM", "US", "UY", "UZ", "VA", "VC", "VE", // 230
	"VG", "VI", "VN", "VU", "WF", "WS", "YE", "YT", "RS", "ZA", "ZM", // 241 (RS SERBIA new) old: YU YUGOSLAVIA
	"ME", "ZW", "A1", "A2", "O1", "AX", "GG", "IM", "JE", "BL", "MF", // 252 (ME MONTENEGRO new)
	"BQ", "SS", "Unknown", // BQ BONAIRE, SINT EUSTATIUS AND SABA, SS SOUTH SUDAN
} // size 256

// getISO returns the ISO code for a given country ID.
// Returns empty string if the ID is out of bounds or 0.
// Internal function.
func getISO(id uint32) string {
	// Use uint32 for comparison safety, though IDs are typically uint8
	if id > 0 && id < uint32(len(id2iso)) {
		return id2iso[id]
	}
	return "" // Return empty for ID 0 or out of range
}
