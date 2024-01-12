package lsp

import (
	cmap "github.com/orcaman/concurrent-map/v2"
)

type Cache struct {
	pkgs cmap.ConcurrentMap[string, *Package]
}

func (c *Cache) lookupSymbol(pkgPath, symbol string) (*Symbol, bool) {
	pkg, ok := c.pkgs.Get(pkgPath)
	if !ok {
		return nil, false
	}

	for _, s := range pkg.Symbols {
		if s.Name == symbol {
			return s, true
		}
	}
	return nil, false
}

func NewCache() *Cache {
	return &Cache{
		pkgs: cmap.New[*Package](),
	}
}

func (s *server) UpdateCache(pkgPath string) {
	files, err := ListGnoFiles(pkgPath)
	if err != nil {
		// Ignore error
		return
	}
	symbols := []*Symbol{}
	for _, file := range files {
		symbols = append(symbols, getSymbols(file)...)
	}
	s.cache.pkgs.Set(pkgPath, &Package{
		Name:       pkgPath,
		ImportPath: "",
		Symbols:    symbols,
	})
}
