package licenses

import "strings"

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) List(query string) []PackageSummary {
	packages := s.repository.List()

	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return packages
	}

	result := make([]PackageSummary, 0, len(packages))

	for _, pkg := range packages {
		if strings.Contains(strings.ToLower(pkg.Name), query) ||
			strings.Contains(strings.ToLower(pkg.License), query) {
			result = append(result, pkg)
		}
	}

	return result
}

func (s *Service) Get(name string) (Package, bool) {
	return s.repository.Find(name)
}
