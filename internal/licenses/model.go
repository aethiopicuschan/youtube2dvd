package licenses

// Package represents a third-party dependency and its license.
type Package struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
	Text       string `json:"text"`
}

// PackageSummary represents license information without the full text.
type PackageSummary struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}

// Manifest represents the generated license manifest.
type Manifest struct {
	Packages []Package `json:"packages"`
}
