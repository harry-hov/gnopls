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
	// TODO: Unify `GetPackageInfo()` and `PackageFromDir()`?
	pkg, err := PackageFromDir(pkgPath, false)
	if err != nil {
		return
	}
	pkginfo, err := GetPackageInfo(pkgPath)
	if err != nil {
		return
	}

	tc, errs := NewTypeCheck()
	tc.cfg.Importer = tc // set typeCheck importer
	res := pkginfo.TypeCheck(tc)

	// Mutate `res.err` with `errs`, as `res.err` contains
	// only the first error found.
	res.err = *errs

	pkg.TypeCheckResult = res // set typeCheck result
	s.cache.pkgs.Set(pkgPath, pkg)
}
