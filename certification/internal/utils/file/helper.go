package file

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	certutils "github.com/redhat-openshift-ecosystem/openshift-preflight/certification/utils"

	log "github.com/sirupsen/logrus"
)

func DownloadFile(filename string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func Unzip(bzipfile string, destination string) error {

	f, err := os.Open(bzipfile)
	if err != nil {
		return err
	}
	defer f.Close()

	in := bzip2.NewReader(f)

	out, err := os.Create(destination)

	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)

	if err != nil {
		return err
	}
	out.Close()
	return nil
}

func WriteFileToArtifactsPath(filename, contents string) (string, error) {
	fullFilePath := filepath.Join(certutils.ArtifactPath(), filename)

	err := ioutil.WriteFile(fullFilePath, []byte(contents), 0644)
	if err != nil {
		return fullFilePath, err
	}
	return fullFilePath, nil
}

// Untar takes a destination path and a reader; a tar reader loops over the tarfile
// creating the file structure at 'dst' along the way, and writing any files
func Untar(dst string, r io.Reader) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(dst, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()

			// if it's a link create it
		case tar.TypeSymlink:
			// head, _ := tar.FileInfoHeader(header.FileInfo(), "link")
			log.Println(fmt.Sprintf("Old: %s, New: %s", header.Linkname, header.Name))
			err := os.Symlink(header.Linkname, filepath.Join(dst, header.Name))
			if err != nil {
				log.Println(fmt.Sprintf("Error creating link: %s. Ignoring.", header.Name))
				continue
			}
		}
	}
}
