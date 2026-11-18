package licenses

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Repository struct {
	packages []Package
}

func NewRepository() (*Repository, error) {
	data, err := generated.ReadFile("generated/licenses.json")
	if err != nil {
		return nil, fmt.Errorf("read license manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode license manifest: %w", err)
	}
	for i := range manifest.Packages {
		manifest.Packages[i].LicenseURL = strings.ReplaceAll(
			manifest.Packages[i].LicenseURL,
			".git/blob/",
			"/blob/",
		)
	}

	sort.Slice(manifest.Packages, func(i, j int) bool {
		return manifest.Packages[i].Name <
			manifest.Packages[j].Name
	})

	return &Repository{
		packages: manifest.Packages,
	}, nil
}

func (r *Repository) List() []PackageSummary {
	result := make([]PackageSummary, 0, len(r.packages))

	for _, pkg := range r.packages {
		result = append(result, PackageSummary{
			Name:       pkg.Name,
			Version:    pkg.Version,
			License:    pkg.License,
			LicenseURL: pkg.LicenseURL,
		})
	}

	return result
}

func (r *Repository) Find(name string) (Package, bool) {
	for _, pkg := range r.packages {
		if pkg.Name == name {
			return pkg, true
		}
	}

	return Package{}, false
}
