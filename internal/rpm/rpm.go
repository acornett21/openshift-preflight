package rpm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	rpmdb "github.com/knqyf263/go-rpmdb/pkg"
	// This pulls in the sqlite dependency
	_ "github.com/glebarez/go-sqlite"
)

// GetPackageList returns the list of packages in the rpm database from
// /var/lib/rpm/rpmdb.sqlite, /var/lib/rpm/Packages or /usr/lib/sysimage/rpm/rpmdb.sqlite.
// If neither exists, this returns an error of type os.ErrNotExists
func GetPackageList(ctx context.Context, basePath string) ([]*rpmdb.PackageInfo, error) {
	rpmdbPaths := []string{
		filepath.Join(basePath, "var", "lib", "rpm", "rpmdb.sqlite"),
		filepath.Join(basePath, "var", "lib", "rpm", "Packages"),
		filepath.Join(basePath, "usr", "lib", "sysimage", "rpm", "rpmdb.sqlite"),
	}

	var rpmdbPath string
	var lastErr error
	for _, path := range rpmdbPaths {
		if _, err := os.Stat(path); err == nil {
			rpmdbPath = path
			break
		} else {
			lastErr = err
		}
	}

	if rpmdbPath == "" {
		return nil, fmt.Errorf("could not find rpm db/packages: %v", lastErr)
	}

	db, err := rpmdb.Open(rpmdbPath)
	if err != nil {
		return nil, fmt.Errorf("could not open rpm db: %v", err)
	}
	defer db.Close()

	pkgList, err := db.ListPackages()
	if err != nil {
		return nil, fmt.Errorf("could not list packages: %v", err)
	}

	return pkgList, nil
}
